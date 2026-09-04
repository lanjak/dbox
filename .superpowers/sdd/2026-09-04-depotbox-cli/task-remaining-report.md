# Depotbox CLI remaining tasks report

Date: 2026-09-04 (Australia/Sydney)

## Scope

Fixed all findings in final review 2. The follow-up changes keep menu input line-scoped, apply edited configuration to the active App, restrict API-key redirects to the configured origin, reject Unix config symlinks, sanitize Windows device names, and expand the real HTTP integration workflow.

## Files changed

- `internal/config/config.go` and tests: validation, platform config path, JSON persistence, parent creation, and Unix `0600` permissions.
- `internal/api/client.go` and tests: authenticated search, fixes, game download, fix download, JSON/query encoding, response decoding, bounded non-2xx errors, and exact `X-API-Key` header.
- `internal/ratelimit/limiter.go` and tests: Starter 60/minute and Pro 120/minute rolling limiter with context cancellation.
- `internal/download/download.go` and tests: filename sanitization, temporary streaming, cleanup, atomic rename, and overwrite protection.
- `internal/cli/cli.go` and tests: setup, menu output, search/fix selection, direct download, config display/edit, and secret-safe output.
- `internal/app/app.go` and tests: API/download service wiring and configured download directory.
- `internal/integration/cli_test.go`: in-process HTTP workflow coverage for first-run setup, search/download, fixes list/download, direct AppID download, menu config changes, API errors, and timeout behavior.
- `README.md`: setup, commands, rate limits, storage, and security notes.
- Constructors for `config`, `api`, and `download` are now available for downstream callers.
- No-argument mode now runs a real menu loop. Setup reads one line at a time, masks TTY API-key input, and supports non-terminal readers.
- Menu search exposes DLC, availability, and protection filters. Menu fix search exposes tags.
- Selected game downloads use sanitized game names. API failures omit response bodies and redact keys. Requests have a 30-second timeout.
- No-overwrite downloads use an atomic hard-link commit, so a collision cannot replace an existing file.
- Menu confirmations and config edits consume one input line at a time. Config edits update the active App and its limiter/API key.
- Redirects are allowed only within the configured scheme and host. Unix config saves use `O_NOFOLLOW`; Windows reserved device names are rewritten.

## Verification

Commands run from the worktree:

```text
gofmt -w changed Go files                          PASS
go test ./...                                      PASS (all packages)
go test -race ./...                                PASS (all packages)
go vet ./...                                       PASS
go build -o dbox ./cmd/dbox                       PASS
git diff --check                                   PASS
```

The test suite uses only `httptest` servers, temporary directories, and placeholder test keys. No real API key is included or logged.

- All findings from final review 2 are addressed.
- Tests cover menu looping and line-scoped confirmations, active config changes, setup fallback and secret-safe output, config view without input reads, filters, fix tags, selected-game names, reserved Windows names, Unix symlink rejection, same-origin and cross-host redirects, API error redaction, timeout behavior, and collision preservation.
- No real API key was added, printed, or logged.
- The requested test, race, vet, build, formatting, and diff checks passed in this worktree on 2026-09-04 (Australia/Sydney).
