# Depotbox CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a dependency-light Go CLI that configures Depotbox access, searches games and fixes, and safely downloads selected or directly addressed ZIP files.

**Architecture:** Use focused packages for configuration, API requests, rate limiting, downloads, and CLI workflows. Keep HTTP and filesystem behavior behind small interfaces so tests can use an in-process HTTP server and temporary directories. A thin `cmd/dbox` entry point selects interactive or direct-command behavior.

**Tech Stack:** Go 1.27.1, standard library only, `net/http`, `encoding/json`, `os`, `path/filepath`, `time`, and `testing`.

## Global Constraints

- Base URL: `https://depotbox.org`.
- Every API request sends the configured key in `X-API-Key`.
- Never hardcode, print, or log a real API key.
- Store config as user-private JSON; use mode `0600` on Unix-like systems.
- Starter permits 60 requests per minute; Pro permits 120 requests per minute.
- Downloads stream to a temporary file and rename only after success.
- Existing final files are not silently overwritten.
- Use TDD: each production behavior gets a failing test before implementation.
- Do not add third-party dependencies.

---

### Task 1: Initialize the Go module and package boundaries

**Files:**
- Create: `go.mod`
- Create: `cmd/dbox/main.go`
- Create: `internal/config/config.go`
- Create: `internal/api/client.go`
- Create: `internal/ratelimit/limiter.go`
- Create: `internal/download/download.go`
- Create: `internal/cli/cli.go`

**Interfaces:**
- Establish package names and constructors without implementing behavior.
- `main` will call `cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)` and exit non-zero on error.

- [ ] **Step 1: Write the module and package smoke test first**

Create `internal/cli/cli_test.go` with:

```go
func TestRunWithNoArgumentsRequiresConfiguration(t *testing.T) {
	if err := Run(nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("expected configuration error")
	}
}
```

- [ ] **Step 2: Run the test and verify it fails because `Run` is undefined**

Run `go test ./internal/cli`.

- [ ] **Step 3: Add the minimal module and stubs**

Create `go.mod` with `module github.com/rufi/dbox` and `go 1.27`. Add the package declarations, `cli.Run` returning `errors.New("configuration is not set up")`, and `main.go` that reports errors to stderr.

- [ ] **Step 4: Run the smoke test and build**

Run `go test ./...` and `go build ./...`; both must exit 0.

- [ ] **Step 5: Commit**

Run `git add go.mod cmd internal && git commit -m "build: initialize Depotbox CLI"`.

### Task 2: Add secure configuration persistence and first-run setup

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- `type Plan string` with `Starter` and `Pro`.
- `type Config struct { APIKey, DownloadDir string; Plan Plan }`.
- `func Load(path string) (Config, error)`.
- `func (c Config) Validate() error`.
- `func Save(path string, c Config) error`.
- `func DefaultPath() (string, error)`.

- [ ] **Step 1: Write failing tests**

Test invalid plans and empty keys, JSON round-trip, creation of the parent directory, and Unix mode `0600` where supported.

```go
func TestValidateRejectsUnsupportedPlan(t *testing.T) {
	c := Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: Plan("enterprise")}
	if err := c.Validate(); err == nil { t.Fatal("expected unsupported plan error") }
}
```

- [ ] **Step 2: Run `go test ./internal/config` and verify the new tests fail**

- [ ] **Step 3: Implement validation, JSON load/save, platform config path, and restrictive permissions**

Use `os.UserConfigDir`, `filepath.Join(dir, "dbox", "config.json")`, `os.MkdirAll`, `json.Decoder`, and `os.OpenFile` with `0600`.

- [ ] **Step 4: Run `go test ./internal/config` and verify all tests pass**

- [ ] **Step 5: Commit**

Run `git add internal/config && git commit -m "feat: persist secure CLI configuration"`.

### Task 3: Implement API types, authenticated requests, and rate limiting

**Files:**
- Modify: `internal/api/client.go`
- Modify: `internal/ratelimit/limiter.go`
- Create: `internal/api/client_test.go`
- Create: `internal/ratelimit/limiter_test.go`

**Interfaces:**
- `type Client struct { BaseURL string; APIKey string; HTTPClient *http.Client; Limiter *ratelimit.Limiter }`.
- `func (c *Client) Search(ctx context.Context, request SearchRequest) (SearchResponse, error)`.
- `func (c *Client) Fixes(ctx context.Context, query, tags string) (FixesResponse, error)`.
- `func (c *Client) DownloadGame(ctx context.Context, appID string, dst io.Writer) error`.
- `func (c *Client) DownloadFix(ctx context.Context, id, file string, dst io.Writer) error`.
- `func New(plan config.Plan) *Limiter` and `func (l *Limiter) Wait(ctx context.Context) error`.

- [ ] **Step 1: Write failing HTTP tests**

Use `httptest.NewServer` to assert method, path, JSON body, query encoding, and exact `X-API-Key` header. Add response decoding tests for games and fixes, and non-2xx error tests that confirm the key is absent from the returned error.

- [ ] **Step 2: Run `go test ./internal/api ./internal/ratelimit` and verify failure**

- [ ] **Step 3: Implement request helpers and response structs**

Use context-aware requests, `Content-Type: application/json` for POST requests, bounded error-body reads, status checks, and JSON decoding. Apply the limiter before each request.

- [ ] **Step 4: Implement the plan limiter and verify cancellation**

Use a mutex-protected rolling one-minute timestamp queue. Wait until the oldest timestamp expires; return `ctx.Err()` if cancelled.

- [ ] **Step 5: Run package tests and commit**

Run `go test ./internal/api ./internal/ratelimit`; then commit with `git add internal/api internal/ratelimit && git commit -m "feat: add Depotbox API client and rate limiting"`.

### Task 4: Implement safe downloads and filename handling

**Files:**
- Modify: `internal/download/download.go`
- Create: `internal/download/download_test.go`

**Interfaces:**
- `func SanitizeFilename(name string) string`.
- `func SaveStream(dir, filename string, src io.Reader, overwrite bool) (string, error)`.

- [ ] **Step 1: Write failing tests**

Cover path traversal removal, invalid filename characters, empty names, successful stream persistence, failed stream cleanup, and refusal to overwrite when `overwrite` is false.

```go
func TestSaveStreamDoesNotOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.zip")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil { t.Fatal(err) }
	if _, err := SaveStream(dir, "game.zip", strings.NewReader("new"), false); err == nil { t.Fatal("expected conflict") }
}
```

- [ ] **Step 2: Run `go test ./internal/download` and verify failure**

- [ ] **Step 3: Implement safe temporary-file streaming and atomic rename**

Create the temporary file in `dir`, copy with `io.Copy`, close before rename, remove it on every error, and use `os.Rename` only after a successful copy.

- [ ] **Step 4: Run `go test ./internal/download` and verify all tests pass**

- [ ] **Step 5: Commit**

Run `git add internal/download && git commit -m "feat: add safe archive downloads"`.

### Task 5: Build interactive menus and direct command workflows

**Files:**
- Modify: `internal/cli/cli.go`
- Create: `internal/cli/cli_test.go`
- Create: `internal/cli/prompts.go`
- Create: `internal/cli/prompts_test.go`

**Interfaces:**
- `func Run(args []string, in io.Reader, out, errOut io.Writer) error`.
- `type Service interface` exposing search, fixes, and download operations.
- Prompt helpers accept `io.Reader` and `io.Writer`, so tests do not require a terminal.

- [ ] **Step 1: Write failing CLI tests**

Test that `search Cyberpunk` prints numbered results and accepts a selection, `download 70` calls one download without a selection prompt, `fixes` selects a result before downloading, and no-argument mode prints the five menu entries. Use a fake service implementing `Service`; assert calls and output, not fake internals.

- [ ] **Step 2: Run `go test ./internal/cli` and verify failure**

- [ ] **Step 3: Implement argument dispatch and menu loop**

Dispatch `search`, `download`, `fixes`, and `config`; reject missing or extra required arguments with usage text. Make `search` and `fixes` pause for numbered selection, detail display, and a download choice. Make direct `download` call the service once and return.

- [ ] **Step 4: Implement first-run and config prompts**

Read the API key without echo when stdin is a terminal; use a visible reader fallback in non-terminal tests. Prompt for directory and plan, validate, and save. Mask the key in config output.

- [ ] **Step 5: Run CLI tests and commit**

Run `go test ./internal/cli`; then commit with `git add internal/cli && git commit -m "feat: add interactive and direct CLI workflows"`.

### Task 6: Wire the real services and finish the executable

**Files:**
- Modify: `cmd/dbox/main.go`
- Modify: `internal/cli/cli.go`
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`

**Interfaces:**
- `app.New(cfg config.Config, httpClient *http.Client) *app.App` constructs the API client, limiter, and service implementation.
- The CLI receives the service and config store through explicit dependencies.

- [ ] **Step 1: Write failing wiring tests**

Assert that a configured app uses the configured base URL, plan, download directory, and API key when a test HTTP server handles search and download requests.

- [ ] **Step 2: Run `go test ./internal/app ./internal/cli` and verify failure**

- [ ] **Step 3: Implement the application service**

Map selected game names to sanitized archive filenames, pass fix `id` preferentially and `file` as fallback, and direct game downloads to the configured directory.

- [ ] **Step 4: Wire `main` to load config, run setup when absent, construct the app, and call `cli.Run`**

Do not make a network request during setup. Return non-zero errors from `main` without printing secrets.

- [ ] **Step 5: Run all tests and commit**

Run `go test ./...`; then commit with `git add cmd internal/app && git commit -m "feat: wire Depotbox CLI application"`.

### Task 7: Verify the complete release candidate

**Files:**
- Modify: `README.md`
- Create: `internal/integration/cli_test.go`

- [ ] **Step 1: Write an end-to-end test against `httptest.Server`**

Exercise first-run config, `search Cyberpunk`, result selection, selected download, `fixes`, fix selection, and direct `download 70` with temporary directories and a fake server.

- [ ] **Step 2: Run the integration test and verify it fails if any wiring is incomplete**

Run `go test ./internal/integration -v`.

- [ ] **Step 3: Implement only the missing wiring or output corrections**

- [ ] **Step 4: Add README usage and security notes**

Document setup, menu use, direct commands, fix tags, config location, and that users must supply their own API key. Do not include a real key.

- [ ] **Step 5: Run the full verification suite**

Run `go test ./...`, `go vet ./...`, and `go build -o dbox ./cmd/dbox`. Confirm all commands exit 0 and inspect `git diff --check`.

- [ ] **Step 6: Commit the release documentation and integration coverage**

Run `git add README.md internal/integration && git commit -m "test: verify complete Depotbox CLI workflow"`.

## Self-review

- Configuration, first-run setup, and config editing are covered by Tasks 2 and 5.
- Search, game-fix listing, direct downloads, and selected downloads are covered by Tasks 3-7.
- Plan-specific limits and request headers are covered by Task 3.
- Temporary files, cleanup, sanitization, and overwrite behavior are covered by Task 4.
- Error handling and secret redaction are covered by Tasks 3-6.
- The plan contains no unresolved TODO or TBD items and uses consistent package and method names.
