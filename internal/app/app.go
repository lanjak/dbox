package app

import (
	"context"
	"io"
	"net/http"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/config"
	"github.com/rufi/dbox/internal/download"
	"github.com/rufi/dbox/internal/ratelimit"
)

type App struct {
	Config  config.Config
	Client  *api.Client
	BaseURL string
}

func New(cfg config.Config, h *http.Client) *App {
	a := &App{Config: cfg, BaseURL: "https://depotbox.org"}
	a.Client = &api.Client{BaseURL: a.BaseURL, APIKey: cfg.APIKey, HTTPClient: h, Limiter: ratelimit.New(cfg.Plan)}
	return a
}
func (a *App) Search(ctx context.Context, q string) (api.SearchResponse, error) {
	a.Client.BaseURL = a.BaseURL
	return a.Client.Search(ctx, api.SearchRequest{SearchTerm: q})
}
func (a *App) Fixes(ctx context.Context, q, tags string) (api.FixesResponse, error) {
	a.Client.BaseURL = a.BaseURL
	return a.Client.Fixes(ctx, q, tags)
}
func (a *App) DownloadGame(ctx context.Context, id string) (string, error) {
	return a.save(ctx, id+".zip", func(w io.Writer) error { return a.Client.DownloadGame(ctx, id, w) })
}
func (a *App) DownloadFix(ctx context.Context, f api.Fix) (string, error) {
	return a.save(ctx, download.SanitizeFilename(f.File), func(w io.Writer) error { return a.Client.DownloadFix(ctx, f.ID, f.File, w) })
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
