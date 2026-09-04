# Depotbox CLI design

Date: 2026-09-04

## Goal

Build a small Go command-line application for interacting with the Depotbox API. It must support both an interactive menu and direct commands for searching games, downloading game archives, browsing game fixes, and changing local settings.

The application must never include or log a real API key. Users enter their key during first-run setup.

## User experience

Running `dbox` opens a numbered menu:

1. Search games
2. Download by AppID
3. Game fixes
4. Config
5. Exit

If no configuration exists, the application runs setup first. Setup asks for the API key, download directory, and plan (`starter` or `pro`). The directory is created when possible. The key is masked while entered.

The direct commands are:

- `dbox search <term>`: search, show numbered results, pause for selection, show the selected game's details, and offer a download action.
- `dbox download <appid>`: download the archive immediately to the configured directory.
- `dbox fixes [query]`: search game fixes, show numbered results, pause for selection, show fix details, and offer a download action.
- `dbox config`: view and change saved settings.

Search supports a result limit, DLC filtering, protection filtering, and confirmed-availability filtering. Fix browsing supports tags from `online`, `bypass`, and `hypervisor`.

## Configuration

Configuration is stored as JSON in the platform's user configuration directory. On Unix-like systems the file uses mode `0600`; other platforms use their normal user-private configuration conventions.

The stored values are:

- API key
- Download directory
- Plan

Configuration validation rejects an empty key, unsupported plan, and unusable download directory. The config command edits existing values without exposing the key in normal output.

## API client

The API base URL is `https://depotbox.org`. Every request sends the configured key in the `X-API-Key` header.

Endpoints:

- `POST /api/search-games` with `searchTerm`, optional `limit`, `filter_dlc`, `filter_protection`, and `filter_availability` JSON fields.
- `GET /api/game-fixes` with optional URL-encoded `tag` and `q` query parameters.
- `POST /api/direct-download` with an `appid` JSON field.
- `GET /api/game-fixes/download` with the selected fix identifier or clean filename.

The client uses request timeouts, checks HTTP status codes, and decodes server error messages where available. API request construction and response decoding are kept separate from terminal interaction so they can be tested with an in-process HTTP server.

## Downloads

Downloads stream into a temporary file in the configured destination directory. A successful response is renamed to the final sanitized filename only after the stream completes. Failed or interrupted transfers do not become final ZIP files.

Search downloads use the selected AppID and a sanitized name based on the selected game's name. Direct AppID downloads use the AppID in the filename. Game-fix downloads use the clean filename returned by the list endpoint. Existing files are never overwritten silently: interactive mode asks for confirmation, while direct mode returns an error.

## Rate limiting

The client applies a local request limiter before every API request:

- Starter: 60 requests per minute
- Pro: 120 requests per minute

When the local limit is reached, the request waits until it is allowed. The limiter is process-local and is not intended to coordinate multiple copies of the CLI.

## Error handling

The CLI reports actionable errors for missing configuration, invalid input, invalid JSON, authentication failures, non-success API responses, rate-limit waits, timeouts, filesystem failures, and incomplete downloads. It must not print the API key or include it in error text.

## Project structure

Use a small layered Go application:

- Configuration package for persistence and validation.
- API package for authenticated Depotbox requests and response types.
- Rate-limiter package for plan-specific request pacing.
- Download package for safe streaming and filename handling.
- CLI package for commands, menus, prompts, and result selection.
- A thin `main` package for process startup and exit status.

## Verification

Tests will cover configuration validation and persistence, request construction, response decoding, rate limiting, filename sanitization, safe downloads, first-run setup, search selection, direct downloads, game-fix selection, and configuration changes. HTTP tests will use an in-process server and no real API key.

The implementation must pass:

```text
go test ./...
go vet ./...
go build ./...
```

