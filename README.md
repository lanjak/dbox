# Depotbox CLI

`dbox` is a standard-library-only command-line client for Depotbox.

On first run, provide your own Depotbox API key, a download directory, and either `starter` or `pro`. Configuration is saved at the platform user configuration path under `dbox/config.json`; on Unix-like systems the file is mode `0600`. The key is never shown in normal output. This repository contains no real API key.

Usage:

```text
dbox                         # menu
dbox search <term>           # choose a result and download it
dbox download <appid> ...   # download one or more AppIDs sequentially
dbox fixes [query]           # choose and download a game fix
dbox config                  # show non-secret settings
dbox history [search-term]   # show recent searches and downloads
```

`dbox config edit` can enable automatic handoff to the installed `accela` launcher. The launcher receives each completed ZIP as a file argument, which is the same workflow as dropping the ZIP onto ACCELA/ASSella. You can save a preferred Steam library path for your own workflow, or leave it blank so Accela chooses. Use the launcher command override when `accela` is not on PATH.

The interactive menu also accepts multiple AppIDs separated by spaces or commas. Searches, fix searches, and successful or failed downloads are saved in the local `history.json` file beside the config, with the latest 100 entries retained.

Search results show the returned header image URL. If `chafa` is installed, `dbox` also displays a terminal preview; normal terminals still show the URL.

The API uses the configured `X-API-Key` header. Starter permits 60 requests per minute and Pro permits 120. Downloads stream to a temporary file and are renamed only after completion; existing files are not overwritten silently.
