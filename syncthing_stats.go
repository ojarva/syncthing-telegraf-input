package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type FolderConfig struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	RescanIntervalS int    `json:"rescanIntervalS"`
	Type            string `json:"type"`
}

type FolderStats struct {
	Errors            int `json:"errors"`
	GlobalBytes       int `json:"globalBytes"`
	GlobalDeleted     int `json:"globalDeleted"`
	GlobalDirectories int `json:"globalDirectories"`
	GlobalFiles       int `json:"globalFiles"`
	GlobalSymlinks    int `json:"globalSymlinks"`
	GlobalTotalItems  int `json:"globalTotalItems"`
	InSyncBytes       int `json:"inSyncBytes"`
	InSyncFiles       int `json:"inSyncFiles"`
	LocalBytes        int `json:"localBytes"`
	LocalDeleted      int `json:"localDeleted"`
	LocalDirectories  int `json:"localDirectories"`
	LocalFiles        int `json:"localFiles"`
	LocalSymlinks     int `json:"localSymlinks"`
	LocalTotalItems   int `json:"localTotalItems"`
	NeedBytes         int `json:"needBytes"`
	NeedDeletes       int `json:"needDeletes"`
	NeedDirectories   int `json:"needDirectories"`
	NeedFiles         int `json:"needFiles"`
	NeedSymlinks      int `json:"needSymlinks"`
	NeedTotalItems    int `json:"needTotalItems"`
	PullErrors        int `json:"pullErrors"`
}

type Report struct {
	NumFolders     int     `json:"numFolders"`
	NumDevices     int     `json:"numDevices"`
	TotalFiles     int     `json:"totFiles"`
	TotalMiB       int     `json:"totMiB"`
	MaxFolderMiB   int     `json:"folderMaxMiB"`
	Sha256Perf     float64 `json:"sha256Perf"`
	HashPerf       float64 `json:"hashPerf"`
	Uptime         int     `json:"uptime"`
	MemoryUsageMiB int     `json:"memoryUsageMiB"`
}

type ConnectionStatItem struct {
	Address       string    `json:"address"`
	At            time.Time `json:"at"`
	ClientVersion string    `json:"clientVersion"`
	Connected     bool      `json:"connected"`
	Crypto        string    `json:"crypto"`
	InBytesTotal  int       `json:"inBytesTotal"`
	OutBytesTotal int       `json:"outBytesTotal"`
	Paused        bool      `json:"paused"`
	Type          string    `json:"type"`
}

type Connections struct {
	Total       ConnectionStatItem            `json:"total"`
	Connections map[string]ConnectionStatItem `json:"connections"`
}

type DeviceConfig struct {
	DeviceID        string `json:"deviceID"`
	Name            string `json:"name"`
}

type DeviceStatItem struct {
	LastSeen                time.Time `json:"lastSeen"`
	LastConnectionDurationS float64   `json:"lastConnectionDurationS"`
}

type Devices map[string]DeviceStatItem

var server = flag.String("server", "http://localhost:8384", "Syncthing API URL")
var apiKeyFlag = flag.String("apikey", "", "Syncthing API key")
var useFullReportFlag = flag.Bool("use-full-report", false, "Add extra stats from svc/report. Somewhat slow/heavy.")

func makeRequest(apiKey string, url string) (*http.Response, error) {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", *server, url), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create HTTP request: %s", err)
	}
	req.Header.Add("X-API-Key", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %s", err)
	}
	return resp, nil
}

func handleSystemConnections(apiKey string, wg *sync.WaitGroup) error {
	defer wg.Done()
	var cutOffTime = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	resp, err := makeRequest(apiKey, "rest/system/connections")
	if err != nil {
		return err
	}

	var stats Connections
	err = json.NewDecoder(resp.Body).Decode(&stats)
	if err != nil {
		return fmt.Errorf("invalid response body: %s", err)
	}
	numberOfConnections := len(stats.Connections)
	var paused int
	if stats.Total.Paused {
		paused = 1
	} else {
		paused = 0
	}
	fmt.Printf("syncthing_connection_totals number_of_connections=%d,in_bytes=%d,out_bytes=%d,paused=%d\n", numberOfConnections, stats.Total.InBytesTotal, stats.Total.OutBytesTotal, paused)

	for connectionId, connectionStat := range stats.Connections {
		if cutOffTime.Before(connectionStat.At) {
			// This connection has likely been updated.
			var connected int
			var paused int
			if connectionStat.Paused {
				paused = 1
			}
			if connectionStat.Connected {
				connected = 1
			}
			fmt.Printf("syncthing_connection,client_id=%s connected=%d,paused=%d,in_bytes=%d,out_bytes=%d\n", connectionId, connected, paused, connectionStat.InBytesTotal, connectionStat.OutBytesTotal)
		}
	}
	return nil
}

func handleDevices(apiKey string, wg *sync.WaitGroup) error {
	defer wg.Done()
	resp, err := makeRequest(apiKey, "rest/config/devices")
	if err != nil {
		return err
	}
	var deviceConfigs []DeviceConfig
	err = json.NewDecoder(resp.Body).Decode(&deviceConfigs)
	if err != nil {
		return fmt.Errorf("invalid response body: %s", err)
	}

	var deviceNames = make(map[string]string);
	for _, device := range deviceConfigs {
		deviceNames[device.DeviceID] = device.Name;
	}

	var cutOffTime = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	resp, err = makeRequest(apiKey, "rest/stats/device")
	if err != nil {
		return err
	}

	var stats Devices
	err = json.NewDecoder(resp.Body).Decode(&stats)
	if err != nil {
		return fmt.Errorf("invalid response body: %s", err)
	}
	numberOfDevices := len(stats)
	fmt.Printf("syncthing_device_totals number_of_devices=%d\n", numberOfDevices)

	for deviceId, deviceStat := range stats {
		if cutOffTime.Before(deviceStat.LastSeen) {
			fmt.Printf("syncthing_device,device_id=%s,device_name=%s last_seen=%f,last_connection_duration=%f\n",
				deviceId, strings.Replace(deviceNames[deviceId], " ", "\\ ", -1), deviceStat.LastSeen.Sub(cutOffTime).Seconds(), deviceStat.LastConnectionDurationS)
		}
	}
	return nil
}

func handleFolderStats(apiKey string, folderConfig FolderConfig, wg *sync.WaitGroup) {
	defer wg.Done()
	resp, err := makeRequest(apiKey, fmt.Sprintf("rest/db/status?folder=%s", folderConfig.ID))
	if err != nil {
		os.Stderr.Write([]byte(fmt.Sprintf("Unable to read status for %s: %s", folderConfig.ID, err)))
		return
	}
	var stats FolderStats
	err = json.NewDecoder(resp.Body).Decode(&stats)
	if err != nil {
		os.Stderr.Write([]byte(fmt.Sprintf("invalid response body: %s", err)))
		return
	}
	fmt.Printf("syncthing_folder,folder_id=%s,folder_label=%s rescanInterval=%d,errors=%d,global_bytes=%d,global_deleted=%d,global_directories=%d,global_files=%d,global_symlinks=%d,global_total_items=%d,insync_bytes=%d,insync_files=%d,local_bytes=%d,local_deleted=%d,local_directories=%d,local_files=%d,local_symlinks=%d,local_total_items=%d,need_bytes=%d,need_deletes=%d,need_directories=%d,need_files=%d,need_symlinks=%d,need_total_items=%d,pull_errors=%d\n", folderConfig.ID, strings.Replace(folderConfig.Label, " ", "\\ ", -1), folderConfig.RescanIntervalS, stats.Errors, stats.GlobalBytes, stats.GlobalDeleted, stats.GlobalDirectories, stats.GlobalFiles, stats.GlobalSymlinks, stats.GlobalTotalItems, stats.InSyncBytes, stats.InSyncFiles, stats.LocalBytes, stats.LocalDeleted, stats.LocalDirectories, stats.LocalFiles, stats.LocalSymlinks, stats.LocalTotalItems, stats.NeedBytes, stats.NeedDeletes, stats.NeedDirectories, stats.NeedFiles, stats.NeedSymlinks, stats.NeedTotalItems, stats.PullErrors)
}

func handleFolders(apiKey string, wg *sync.WaitGroup) error {
	defer wg.Done()
	resp, err := makeRequest(apiKey, "rest/config/folders")
	if err != nil {
		return err
	}
	var folderConfig []FolderConfig
	err = json.NewDecoder(resp.Body).Decode(&folderConfig)
	if err != nil {
		return fmt.Errorf("invalid response body: %s", err)
	}
	for _, folder := range folderConfig {
		wg.Add(1)
		go handleFolderStats(apiKey, folder, wg)
	}
	return nil
}

func handleReport(apiKey string, wg *sync.WaitGroup) error {
	defer wg.Done()
	resp, err := makeRequest(apiKey, "rest/svc/report")
	if err != nil {
		return err
	}
	var stats Report
	err = json.NewDecoder(resp.Body).Decode(&stats)
	if err != nil {
		return fmt.Errorf("invalid response body: %s", err)
	}
	fmt.Printf("syncthing_report num_folders=%d,num_devices=%d,total_files=%d,total_mib=%d,max_folder_mib=%d,sha256perf=%f,hashperf=%f,uptime=%d,memory_usage_mib=%d\n", stats.NumFolders, stats.NumDevices, stats.TotalFiles, stats.TotalMiB, stats.MaxFolderMiB, stats.Sha256Perf, stats.HashPerf, stats.Uptime, stats.MemoryUsageMiB)
	return nil
}

func wrapHandler(handler func(string, *sync.WaitGroup) error, apiKey string, wg *sync.WaitGroup) {
	err := handler(apiKey, wg)
	if err != nil {
		os.Stderr.Write([]byte(fmt.Sprintf("Failed: %s", err)))
	}
}

func main() {

	flag.Parse()
	if *apiKeyFlag == "" {
		fmt.Println("Invalid API key")
		os.Exit(1)
	}
	var wg sync.WaitGroup

	allHandlers := []func(string, *sync.WaitGroup) error{handleFolders, handleSystemConnections, handleDevices}
	if *useFullReportFlag {
		allHandlers = append(allHandlers, handleReport)
	}
	for _, handler := range allHandlers {
		wg.Add(1)
		go wrapHandler(handler, *apiKeyFlag, &wg)
	}
	wg.Wait()
}
