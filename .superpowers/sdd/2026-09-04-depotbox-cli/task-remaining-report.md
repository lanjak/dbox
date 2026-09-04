# Depotbox CLI remaining tasks report

Date: 2026-09-04 (Australia/Sydney)

## Scope

Implemented Tasks 2 through 7 from the approved Depotbox CLI plan. Task 1 remains in its existing commit.

## Files changed

- `internal/config/config.go` and tests: validation, platform config path, JSON persistence, parent creation, and Unix `0600` permissions.
- `internal/api/client.go` and tests: authenticated search, fixes, game download, fix download, JSON/query encoding, response decoding, bounded non-2xx errors, and exact `X-API-Key` header.
- `internal/ratelimit/limiter.go` and tests: Starter 60/minute and Pro 120/minute rolling limiter with context cancellation.
- `internal/download/download.go` and tests: filename sanitization, temporary streaming, cleanup, atomic rename, and overwrite protection.
- `internal/cli/cli.go` and tests: setup, menu output, search/fix selection, direct download, config display/edit, and secret-safe output.
- `internal/app/app.go` and tests: API/download service wiring and configured download directory.
- `internal/integration/cli_test.go`: in-process HTTP workflow coverage.
- `README.md`: setup, commands, rate limits, storage, and security notes.

## Verification

Commands run from the worktree:

```text
go test ./internal/config                         PASS
go test ./internal/api ./internal/ratelimit       PASS after implementation
go test ./internal/download                        PASS after implementation
go test ./internal/app ./internal/cli              PASS after implementation
go test ./...                                      PASS
go vet ./...                                       PASS
go build -o dbox ./cmd/dbox                        PASS
git diff --check                                   PASS
gofmt -w .                                         PASS
```

The test suite uses only `httptest` servers, temporary directories, and placeholder test keys. No real API key is included or logged.

## Concerns

- The API response schema is represented by the fields required by the plan. If Depotbox adds fields or changes response envelopes, the client types may need extension.
- Terminal-specific hidden input is not implemented with platform-specific echo control because the project is standard-library-only; setup output never echoes the supplied key.
- The CLI menu currently prints its five entries and direct workflows are fully wired; a future iteration could add a persistent interactive loop around those same handlers.
