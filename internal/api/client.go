package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rufi/dbox/internal/ratelimit"
)

type Client struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
	Limiter         *ratelimit.Limiter
}
type SearchRequest struct {
	SearchTerm         string `json:"searchTerm"`
	Limit              int    `json:"limit,omitempty"`
	FilterDLC          bool   `json:"filter_dlc,omitempty"`
	FilterProtection   string `json:"filter_protection,omitempty"`
	FilterAvailability bool   `json:"filter_availability,omitempty"`
}
type Game struct {
	AppID       string `json:"appid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Available   bool   `json:"available,omitempty"`
}
type SearchResponse struct {
	Games []Game `json:"games"`
}
type Fix struct {
	ID          string `json:"id"`
	File        string `json:"file"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type FixesResponse struct {
	Fixes []Fix `json:"fixes"`
}

func (c *Client) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	var out SearchResponse
	err := c.json(ctx, http.MethodPost, "/api/search-games", req, &out)
	return out, err
}
func (c *Client) Fixes(ctx context.Context, query, tags string) (FixesResponse, error) {
	path := "/api/game-fixes"
	q := url.Values{}
	if tags != "" {
		q.Set("tag", tags)
	}
	if query != "" {
		q.Set("q", query)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out FixesResponse
	err := c.request(ctx, http.MethodGet, path, nil, &out)
	return out, err
}
func (c *Client) DownloadGame(ctx context.Context, appID string, dst io.Writer) error {
	return c.json(ctx, http.MethodPost, "/api/direct-download", map[string]string{"appid": appID}, dst)
}
func (c *Client) DownloadFix(ctx context.Context, id, file string, dst io.Writer) error {
	q := url.Values{}
	if id != "" {
		q.Set("id", id)
	} else {
		q.Set("file", file)
	}
	return c.request(ctx, http.MethodGet, "/api/game-fixes/download?"+q.Encode(), nil, dst)
}
func (c *Client) json(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	return c.request(ctx, method, path, r, out)
}
func (c *Client) request(ctx context.Context, method, path string, body io.Reader, out any) error {
	if c.Limiter != nil {
		if err := c.Limiter.Wait(ctx); err != nil {
			return err
		}
	}
	h := c.HTTPClient
	if h == nil {
		h = &http.Client{}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("API request failed (%s): %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
