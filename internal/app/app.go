package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"git.lanjak.net/rufi/dbox/internal/api"
	"git.lanjak.net/rufi/dbox/internal/config"
	"git.lanjak.net/rufi/dbox/internal/download"
	"git.lanjak.net/rufi/dbox/internal/handoff"
	"git.lanjak.net/rufi/dbox/internal/history"
	"git.lanjak.net/rufi/dbox/internal/ratelimit"
	"git.lanjak.net/rufi/dbox/internal/steam"
)

const DefaultBaseURL = "https://depotbox.lol"

type App struct {
	Config  config.Config
	Client  *api.Client
	History *history.Store
	catalog *catalog
	// Progress, when set, receives the number of bytes saved by each write.
	Progress func(n int64)
}

func NewWithClient(cfg config.Config, baseURL string, h *http.Client) *App {
	a := &App{Config: cfg}
	a.Client = api.New(baseURL, cfg.APIKey, h)
	a.Client.Limiter = ratelimit.New(cfg.Plan)
	return a
}

func New(cfg config.Config, h *http.Client) *App {
	a := NewWithClient(cfg, DefaultBaseURL, h)
	if path, err := config.HistoryPath(); err == nil {
		a.History = history.New(path, 100)
	}
	if dir, err := os.UserCacheDir(); err == nil {
		a.catalog = newCatalog(filepath.Join(dir, "dbox", "catalog.json"))
	}
	return a
}
func (a *App) UpdateConfig(cfg config.Config) {
	a.Config = cfg
	a.Client.APIKey = cfg.APIKey
	a.Client.Limiter = ratelimit.New(cfg.Plan)
}
func (a *App) Search(ctx context.Context, q string) (api.SearchResponse, error) {
	return a.SearchWithFilters(ctx, api.SearchRequest{SearchTerm: q})
}

func (a *App) SearchWithFilters(ctx context.Context, req api.SearchRequest) (api.SearchResponse, error) {
	r, err := a.Client.Search(ctx, req)
	if err == nil && len(r.Games) == 0 {
		r, err = a.closeMatches(ctx, req)
	}
	a.record(history.Entry{Kind: "search", Query: req.SearchTerm, Status: status(err)})
	return r, err
}
func (a *App) Fixes(ctx context.Context, q, tags string) (api.FixesResponse, error) {
	r, err := a.Client.Fixes(ctx, q, tags)
	a.record(history.Entry{Kind: "fix-search", Query: q, Status: status(err)})
	return r, err
}
func (a *App) DownloadGame(ctx context.Context, id string) (string, error) {
	p, err := a.save(ctx, id+".zip", func(ctx context.Context, w io.Writer) error { return a.Client.DownloadGame(ctx, id, w) })
	a.record(history.Entry{Kind: "download", AppID: id, Path: p, Status: status(err)})
	return p, err
}

func (a *App) DownloadGameNamed(ctx context.Context, id, name string) (string, error) {
	p, err := a.save(ctx, download.SanitizeFilename(name)+".zip", func(ctx context.Context, w io.Writer) error { return a.Client.DownloadGame(ctx, id, w) })
	a.record(history.Entry{Kind: "download", AppID: id, Name: name, Path: p, Status: status(err)})
	return p, err
}

// DownloadBatch saves up to api.MaxBatch games as one ZIP.
func (a *App) DownloadBatch(ctx context.Context, ids []string, merge bool) (string, error) {
	name := fmt.Sprintf("DepotBox_Batch_%s_%d-games_%s.zip", ids[0], len(ids), time.Now().Format("20060102-150405"))
	p, err := a.save(ctx, name, func(ctx context.Context, w io.Writer) error { return a.Client.DownloadBatch(ctx, ids, merge, w) })
	a.record(history.Entry{Kind: "batch", AppID: fmt.Sprintf("%d games", len(ids)), Path: p, Status: status(err)})
	return p, err
}

func (a *App) DownloadLua(ctx context.Context, id string) (string, error) {
	p, err := a.save(ctx, id+".lua", func(ctx context.Context, w io.Writer) error { return a.Client.DownloadLua(ctx, id, w) })
	a.record(history.Entry{Kind: "lua", AppID: id, Path: p, Status: status(err)})
	return p, err
}

func (a *App) DownloadFix(ctx context.Context, f api.Fix) (string, error) {
	p, err := a.save(ctx, download.SanitizeFilename(f.File), func(ctx context.Context, w io.Writer) error { return a.Client.DownloadFix(ctx, f.ID, f.File, w) })
	a.record(history.Entry{Kind: "fix-download", AppID: f.AppID, Name: f.Name, Path: p, Status: status(err)})
	return p, err
}

// Game returns the search data for one AppID, which has the header image and
// the DLC and DRM flags. The details call is the fallback; it fails for some games.
func (a *App) Game(ctx context.Context, id string) (api.Game, error) {
	if r, err := a.Client.Search(ctx, api.SearchRequest{SearchTerm: id, Limit: 10}); err == nil {
		for _, g := range r.Games {
			if string(g.AppID) == id {
				return g, nil
			}
		}
	}
	d, err := a.Client.GameDetails(ctx, id)
	return d.Game, err
}

func (a *App) GameDetails(ctx context.Context, id string) (api.GameDetails, error) {
	return a.Client.GameDetails(ctx, id)
}
func (a *App) Availability(ctx context.Context, ids []string) ([]api.Availability, error) {
	return a.Client.Availability(ctx, ids)
}
func (a *App) Usage(ctx context.Context) (api.Usage, error) { return a.Client.Usage(ctx) }
func (a *App) Stats(ctx context.Context) (api.ServiceStats, error) {
	return a.Client.Stats(ctx)
}
func (a *App) RequestGame(ctx context.Context, id string) (api.GameRequest, error) {
	r, err := a.Client.RequestGame(ctx, id)
	a.record(history.Entry{Kind: "request", AppID: id, Name: r.GameName, Status: status(err)})
	return r, err
}
func (a *App) GameRequests(ctx context.Context) ([]api.GameRequest, error) {
	return a.Client.GameRequests(ctx)
}
func (a *App) Lists(ctx context.Context, page int) (api.ListsPage, error) {
	return a.Client.Lists(ctx, page)
}
func (a *App) List(ctx context.Context, id string) (api.List, error) {
	return a.Client.List(ctx, id)
}

func (a *App) HistoryEntries() ([]history.Entry, error) {
	if a.History == nil {
		return nil, nil
	}
	return a.History.Load()
}

func (a *App) Handoff(path string) error {
	if !a.HandoffEnabled() {
		return nil
	}
	switch a.Config.HandoffTarget {
	case config.HandoffWindowsSteam:
		return handoff.InstallSteamZip(path, a.Config.SteamRoot)
	}
	return handoff.Launch(a.Config.HandoffTarget, a.Config.HandoffCommand, path)
}

// InstalledApps lists the games that Steam knows about. A game is marked as
// managed when dbox downloaded it or when a client mod has a Lua script for it,
// so the ones that did not come from a purchase stand out.
func (a *App) InstalledApps() ([]steam.App, error) {
	root, err := steam.Root(a.Config.SteamRoot)
	if err != nil {
		return nil, err
	}
	apps := steam.InstalledApps(root)
	fromDbox := map[string]bool{}
	if entries, err := a.HistoryEntries(); err == nil {
		for _, e := range entries {
			if e.AppID == "" || e.Status == "failed" || !digitsOnly(e.AppID) {
				continue
			}
			if e.Kind == "download" || e.Kind == "lua" {
				fromDbox[e.AppID] = true
			}
		}
	}
	for i := range apps {
		if fromDbox[apps[i].AppID] {
			apps[i].Managed = true
		}
	}
	return apps, nil
}

func (a *App) HandoffEnabled() bool {
	return a.Config.HandoffTarget != "" && a.Config.HandoffTarget != config.HandoffNone
}

func (a *App) record(entry history.Entry) {
	if a.History != nil {
		entry.At = time.Now().UTC()
		_ = a.History.Append(entry)
	}
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func status(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, download.ErrFileExists):
		return "exists"
	}
	return "failed"
}

// save streams fn's output into the download directory. It returns the final
// path also on ErrFileExists, so callers can point at the existing file. The
// existence check runs before fn, so an existing file never costs a request.
func (a *App) save(ctx context.Context, name string, fn func(context.Context, io.Writer) error) (string, error) {
	final := filepath.Join(a.Config.DownloadDir, download.SanitizeFilename(name))
	if _, err := os.Stat(final); err == nil {
		return final, fmt.Errorf("%w: %s", download.ErrFileExists, final)
	}
	// A local save failure cancels the request, so it does not wait for the server build.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { e := fn(ctx, w); w.CloseWithError(e); done <- e }()
	var src io.Reader = r
	if a.Progress != nil {
		src = progressReader{r, a.Progress}
	}
	path, err := download.SaveStream(a.Config.DownloadDir, name, src, false)
	if err != nil {
		cancel()
		r.CloseWithError(err)
	}
	e := <-done
	if err == nil {
		err = e
	}
	if errors.Is(err, download.ErrFileExists) {
		path = final
	}
	return path, err
}

type progressReader struct {
	r  io.Reader
	fn func(int64)
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.fn(int64(n))
	}
	return n, err
}
