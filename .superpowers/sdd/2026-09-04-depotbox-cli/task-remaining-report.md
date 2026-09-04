# Depotbox CLI remaining tasks report

Date: 2026-09-04 (Australia/Sydney)

## Scope

Implemented the approved plan and fixed all HIGH and MEDIUM findings from the final review.

## Files changed

- `internal/config/config.go` and tests: validation, platform config path, JSON persistence, parent creation, and Unix `0600` permissions.
- `internal/api/client.go` and tests: authenticated search, fixes, game download, fix download, JSON/query encoding, response decoding, bounded non-2xx errors, and exact `X-API-Key` header.
- `internal/ratelimit/limiter.go` and tests: Starter 60/minute and Pro 120/minute rolling limiter with context cancellation.
- `internal/download/download.go` and tests: filename sanitization, temporary streaming, cleanup, atomic rename, and overwrite protection.
- `internal/cli/cli.go` and tests: setup, menu output, search/fix selection, direct download, config display/edit, and secret-safe output.
- `internal/app/app.go` and tests: API/download service wiring and configured download directory.
- `internal/integration/cli_test.go`: in-process HTTP workflow coverage.
- `README.md`: setup, commands, rate limits, storage, and security notes.
- Constructors for `config`, `api`, and `download` are now available for downstream callers.
- No-argument mode now runs a real menu loop. Setup reads one line at a time, masks TTY API-key input, and supports non-terminal readers.
- Menu search exposes DLC, availability, and protection filters. Menu fix search exposes tags.
- Selected game downloads use sanitized game names. API failures omit response bodies and redact keys. Requests have a 30-second timeout.
- No-overwrite downloads use an atomic hard-link commit, so a collision cannot replace an existing file.

## Verification

Commands run from the worktree:

```text
gofmt -w .                                         PASS
go test ./...                                      PASS (all packages)
go vet ./...                                       PASS
go build -o dbox ./cmd/dbox                        PASS
git diff --check                                   PASS
```

The test suite uses only `httptest` servers, temporary directories, and placeholder test keys. No real API key is included or logged.

## Results

- Tests cover menu looping, setup fallback and secret-safe output, config view without input reads, filters, fix tags, selected-game names, timeout configuration, API error redaction, and collision preservation.
- No real API key was added, printed, or logged.
