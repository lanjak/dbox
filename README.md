# dbox

**DepotBox from your terminal.** Search the DepotBox catalog, download depot packages, Lua scripts and game fixes, browse community lists, and hand finished ZIPs to your installer. `dbox` is one small static binary with no dependencies.

```text
$ dbox search --no-dlc half life
3 result(s)
───────────
  1. Half-Life                                      70
  2. Half-Life 2                                    220
  3. Half-Life 2 RTX                                2477290
Select a game (1-3, Enter to go back): 2
...
⠹ Downloading Half-Life 2  1.2 MB  0:01
✓ Saved ~/Downloads/dbox/Half-Life 2.zip (1.3 MB in 0:01)
```

## Features

- **Search** by name or AppID, with filters for DLC, DRM notices and availability.
- **Download** one game, many games, or a batch of up to 50 games as one ZIP.
- **Lua only**: get the `.lua` script without the manifests.
- **Game info**: details, availability and the DLC list for an AppID.
- **Availability check** for many AppIDs in one request.
- **Game fixes**: find and download online, bypass and hypervisor fixes.
- **Community lists**: browse public lists and download a full list in one step.
- **Game requests**: ask DepotBox to add a missing game, and follow the status of your requests.
- **Usage**: your API usage and live DepotBox service statistics.
- **Handoff**: send each new ZIP to ACCELA or [ASSella](https://github.com/niwia/ASSella), or install it directly into a Windows Steam folder.
- A live progress line, colour output, an interactive menu, and history of your last 100 actions.
- Safe by design: downloads go to a temporary file first, existing files are never replaced, the API key is never printed, and requests stay inside your plan's rate limit.

## Install

### Linux, macOS and FreeBSD

```sh
curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh
```

The installer works on all Linux distributions (Debian, Ubuntu, Fedora, Arch, openSUSE, Alpine, Void, Gentoo, NixOS and others) on x86-64, ARM64, ARMv7, x86 and RISC-V. It:

1. Finds the latest release for your system and CPU.
2. Downloads the static binary and checks its SHA-256 sum.
3. Installs it to `~/.local/bin` (or `/usr/local/bin` when you run it as root).
4. Tells you if that folder is not on your `PATH`.

If no prebuilt binary matches your system, the installer builds `dbox` from source when Go is installed.

Installer options:

```sh
# a specific version
curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh -s -- --version v0.2.0
# a different folder
curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh -s -- --dir ~/bin
# system-wide
curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sudo sh
# remove dbox (your settings stay)
curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh -s -- --uninstall
```

`wget -qO- <url> | sh` also works.

### Windows

Download `dbox-windows-amd64.exe` (or `dbox-windows-arm64.exe`) from the [releases page](https://git.lanjak.net/rufi/dbox/releases), rename it to `dbox.exe`, and put it in a folder on your `PATH`.

### With Go

```sh
go install git.lanjak.net/rufi/dbox/cmd/dbox@latest
```

### Optional: images in the terminal

Install [chafa](https://hpjansson.org/chafa/) to see the game header image when you open a game. The installer shows the command for your package manager.

## First run

Run `dbox`. The first run asks for three settings:

| Setting | Default | Notes |
| --- | --- | --- |
| API key | none | From your DepotBox dashboard. The input is hidden. |
| Download directory | `~/Downloads/dbox` | Created when missing. |
| Plan | `starter` | `starter` (60 requests a minute) or `pro` (120). |

Change them later with `dbox config edit`.

## Usage

Run `dbox` with no arguments for the interactive menu. Each menu item is also a command:

| Command | What it does |
| --- | --- |
| `dbox search [filters] <term>` | Search, pick a game, see its details and download it. |
| `dbox download <appid>...` | Download one or more games, one after another. |
| `dbox download --zip [--merge] <appid>...` | Pack the games into one batch ZIP (50 per ZIP). `--merge` puts all files in one folder. |
| `dbox info <appid>` | Show details, availability and DLCs. |
| `dbox check <appid>...` | Check availability without downloading. |
| `dbox lua <appid>...` | Download only the `.lua` script. |
| `dbox fixes [--tag T] [query]` | Find and download game fixes. `T` is `online`, `bypass` or `hypervisor` (comma-separated). |
| `dbox lists [page]` | Browse community lists. |
| `dbox list <list-id>` | Show one list and download all of its games. |
| `dbox request <appid>` | Ask DepotBox to add a missing game. |
| `dbox requests` | Show your game requests and their status. |
| `dbox usage` | Show your API usage and DepotBox service statistics. |
| `dbox install <archive.zip>` | Send a ZIP to the handoff target. |
| `dbox history [term]` | Show recent searches and downloads. |
| `dbox config [edit]` | Show or change settings. |
| `dbox help`, `dbox version` | Help and version. |

Search filters:

| Flag | Effect |
| --- | --- |
| `--dlc` / `--no-dlc` | Only DLC / no DLC. |
| `--drm` / `--no-drm` | Only games with / without a DRM notice. |
| `--available` | Only games that have a manifest. |
| `--limit N` | Up to `N` results (1-100, default 25). |

In the menu, type the same filter words separated by commas, for example `no-dlc, available`.

### Examples

```sh
dbox search --available elden ring      # search and pick
dbox download 70 220 440                # three ZIPs, one after another
dbox download --zip 70 220 440          # one batch ZIP
dbox check 70 1 220                     # which of these can I get?
dbox lua 70                             # only 70.lua
dbox fixes --tag online elden           # online fixes for ELDEN RING
dbox info 489830                        # details and DLC list
```

When DepotBox does not have a game, `dbox` tells you and offers to send a request for it.

## Handoff

`dbox config edit` can send each new game ZIP to an installer:

| Target | What happens |
| --- | --- |
| `off` | Nothing. This is the default. |
| `accela` | Starts `accela <zip>`, the same as when you drop the ZIP on ACCELA or [ASSella](https://github.com/niwia/ASSella). Set "Handoff command" if the launcher is not `accela` on your `PATH`. |
| `windows-steam` | Copies the `.lua` and `.manifest` files into the Steam folder that you set as "Windows Steam root" (the folder that contains `steam.exe`). An existing `.lua` is backed up first. |

Fix ZIPs are never handed off, because they patch game files and are not depot packages. Use `dbox install <zip>` to hand off a ZIP that you already have.

## Files and privacy

| File | Location |
| --- | --- |
| Settings | `~/.config/dbox/config.json` on Linux, `~/Library/Application Support/dbox/` on macOS, `%AppData%\dbox\` on Windows |
| History | `history.json` in the same folder (last 100 entries) |
| Downloads | The download directory that you set |

The settings file holds your API key. On Linux, macOS and FreeBSD, `dbox` makes it readable only by you (mode `0600`). The key goes only to `https://depotbox.lol` in the `X-API-Key` header. `dbox` never prints it, never writes it to history, and does not follow redirects to other hosts.

## Limits and errors

- `dbox` keeps to your plan's limit (60 or 120 requests a minute). If the server still answers `429 Too Many Requests`, `dbox` waits the time that the server gives and tries again.
- DepotBox builds each package while the download is open. This can take some minutes for a large game, so `dbox` waits up to 15 minutes. The progress line shows "waiting for the server to build the package" during this time.
- `dbox` shows the error message from DepotBox, for example `Forbidden: Invalid API Key.`
- Press Ctrl-C to stop. A stopped download leaves no partial file.
- `dbox` does not replace a file that already exists, and it does not send a request for that file.

## Troubleshooting

| Problem | Fix |
| --- | --- |
| `dbox: command not found` | Add the install folder to your `PATH`. The installer shows the line to add. |
| `load config ...` error | The settings file is damaged. Correct it, or delete it and run `dbox` again to repeat the setup. |
| `DepotBox API error 403` | The key is not valid, not yet approved, or limited to other IP addresses. Look at your DepotBox dashboard. |
| A game shows "not available" | Use `dbox request <appid>` to ask for it. |
| No images | Install `chafa`. Images show only in a terminal, not when the output goes to a file. |

## Build from source

`dbox` uses only the Go standard library.

```sh
git clone https://git.lanjak.net/rufi/dbox.git
cd dbox
go test ./...
go build -o dbox ./cmd/dbox
```

To make release binaries for all platforms, with a `SHA256SUMS` file, run `scripts/build-release.sh v0.2.0`. The files go into `dist/`.

## License

Copyright (C) 2026 Jake Lancaster

`dbox` is free software under the [GNU General Public License v3.0 or later](LICENSE). You can use, copy, change and share it. If you share it or a changed version, you must keep this copyright notice and the license text, and give the source code under the same license.

## Disclaimer

`dbox` is an unofficial client for the DepotBox API. You need your own DepotBox API key. Obey the DepotBox terms and the law in your country.
