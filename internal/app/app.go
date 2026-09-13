package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/config"
	"github.com/rufi/dbox/internal/download"
	"github.com/rufi/dbox/internal/handoff"
	"github.com/rufi/dbox/internal/history"
	"github.com/rufi/dbox/internal/ratelimit"
)

type App struct {
	Config  config.Config
	Client  *api.Client
	BaseURL string
	History *history.Store
}

func NewWithClient(cfg config.Config, baseURL string, h *http.Client) *App {
	a := &App{Config: cfg, BaseURL: baseURL}
	a.Client = api.New(baseURL, cfg.APIKey, h)
	a.Client.Limiter = ratelimit.New(cfg.Plan)
	return a
}

func New(cfg config.Config, h *http.Client) *App {
	a := NewWithClient(cfg, "https://depotbox.org", h)
	if path, err := config.HistoryPath(); err == nil {
		a.History = history.New(path, 100)
	}
	return a
}
func (a *App) UpdateConfig(cfg config.Config) {
	a.Config = cfg
	a.Client.APIKey = cfg.APIKey
	a.Client.Limiter = ratelimit.New(cfg.Plan)
}
func (a *App) Search(ctx context.Context, q string) (api.SearchResponse, error) {
	a.Client.BaseURL = a.BaseURL
	r, err := a.Client.Search(ctx, api.SearchRequest{SearchTerm: q})
	a.record(history.Entry{Kind: "search", Query: q, Status: status(err)})
	return r, err
}

func (a *App) SearchWithFilters(ctx context.Context, req api.SearchRequest) (api.SearchResponse, error) {
	a.Client.BaseURL = a.BaseURL
	r, err := a.Client.Search(ctx, req)
	a.record(history.Entry{Kind: "search", Query: req.SearchTerm, Status: status(err)})
	return r, err
}
func (a *App) Fixes(ctx context.Context, q, tags string) (api.FixesResponse, error) {
	a.Client.BaseURL = a.BaseURL
	r, err := a.Client.Fixes(ctx, q, tags)
	a.record(history.Entry{Kind: "fix-search", Query: q, Status: status(err)})
	return r, err
}
func (a *App) DownloadGame(ctx context.Context, id string) (string, error) {
	p, err := a.save(ctx, id+".zip", func(w io.Writer) error { return a.Client.DownloadGame(ctx, id, w) })
	a.record(history.Entry{Kind: "download", AppID: id, Path: p, Status: status(err)})
	return p, err
}

func (a *App) DownloadGameNamed(ctx context.Context, id, name string) (string, error) {
	filename := download.SanitizeFilename(name) + ".zip"
	p, err := a.save(ctx, filename, func(w io.Writer) error { return a.Client.DownloadGame(ctx, id, w) })
	if errors.Is(err, download.ErrFileExists) {
		p = filepath.Join(a.Config.DownloadDir, filename)
	}
	a.record(history.Entry{Kind: "download", AppID: id, Name: name, Path: p, Status: status(err)})
	return p, err
}
func (a *App) DownloadFix(ctx context.Context, f api.Fix) (string, error) {
	p, err := a.save(ctx, download.SanitizeFilename(f.File), func(w io.Writer) error { return a.Client.DownloadFix(ctx, f.ID, f.File, w) })
	a.record(history.Entry{Kind: "fix-download", Name: f.Name, Path: p, Status: status(err)})
	return p, err
}

func (a *App) HistoryEntries() ([]history.Entry, error) {
	if a.History == nil {
		return nil, nil
	}
	return a.History.Load()
}

func (a *App) Handoff(path string) error {
	return handoff.Launch(a.Config.HandoffTarget, a.Config.HandoffCommand, path)
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

func status(err error) string {
	if err == nil {
		return "success"
	}
	return "failed"
}
func (a *App) save(ctx context.Context, name string, fn func(io.Writer) error) (string, error) {
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { e := fn(w); w.CloseWithError(e); done <- e }()
	path, err := download.SaveStream(a.Config.DownloadDir, name, r, false)
	if err != nil {
		r.CloseWithError(err)
	}
	e := <-done
	if err == nil {
		err = e
	}
	return path, err
}
