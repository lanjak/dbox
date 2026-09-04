# Depotbox CLI

`dbox` is a standard-library-only command-line client for Depotbox.

On first run, provide your own Depotbox API key, a download directory, and either `starter` or `pro`. Configuration is saved at the platform user configuration path under `dbox/config.json`; on Unix-like systems the file is mode `0600`. The key is never shown in normal output. This repository contains no real API key.

Usage:

```text
dbox                         # menu
dbox search <term>           # choose a result and download it
dbox download <appid>       # download immediately
dbox fixes [query]           # choose and download a game fix
dbox config                  # show non-secret settings
```

The API uses the configured `X-API-Key` header. Starter permits 60 requests per minute and Pro permits 120. Downloads stream to a temporary file and are renamed only after completion; existing files are not overwritten silently.
