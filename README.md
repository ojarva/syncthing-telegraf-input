Syncthing statistics to telegraf
================================

Quick start:

1. Build with `go build .`
2. Put `syncthing_stats` binary to some good location, for example `/usr/local/bin`
3. Fetch Syncthing API key from Syncthing GUI, top-right corner Actions - Settings - API Key field in General tab.
4. Configure to telegraf as exec plugin.

```
[[ inputs.exec ]]
  command = "/usr/local/bin/syncthing_stats -apikey YourApiKeyFromStep3"
  data_format = "influx"
```

License
-------

MIT license. For full details, see LICENSE file.