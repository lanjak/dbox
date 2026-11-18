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
	"time"

	"github.com/rufi/dbox/internal/ratelimit"
)

type Client struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
	Limiter         *ratelimit.Limiter
}

const RequestTimeout = 30 * time.Second

func New(baseURL, apiKey string, h *http.Client) *Client {
	if h == nil {
		h = &http.Client{Timeout: RequestTimeout}
	} else if h.Timeout == 0 {
		h.Timeout = RequestTimeout
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, HTTPClient: h}
}

type SearchRequest struct {
	SearchTerm         string `json:"searchTerm"`
	Limit              int    `json:"limit,omitempty"`
	FilterDLC          bool   `json:"filter_dlc,omitempty"`
	FilterProtection   string `json:"filter_protection,omitempty"`
	FilterAvailability bool   `json:"filter_availability,omitempty"`
}
type Game struct {
	AppID                     string  `json:"appid"`
	Name                      string  `json:"name"`
	IsDLC                     bool    `json:"is_dlc,omitempty"`
	DRMNotice                 *string `json:"drm_notice"`
	ExternalUserAccountNotice *string `json:"ext_user_account_notice"`
	HeaderImageURL            string  `json:"header_image_url,omitempty"`
	Description               string  `json:"description,omitempty"`
	Available                 bool    `json:"available,omitempty"`
}

// UnmarshalJSON accepts both the numeric and string AppID forms returned by
// different versions of the Depotbox API.
func (g *Game) UnmarshalJSON(data []byte) error {
	type alias Game
	var aux struct {
		AppID json.RawMessage `json:"appid"`
		*alias
	}
	aux.alias = (*alias)(g)
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.AppID) == 0 || string(aux.AppID) == "null" {
		return nil
	}

	var stringID string
	if err := json.Unmarshal(aux.AppID, &stringID); err == nil {
		g.AppID = stringID
		return nil
	}
	var numberID json.Number
	if err := json.Unmarshal(aux.AppID, &numberID); err != nil {
		return fmt.Errorf("invalid appid: %w", err)
	}
	g.AppID = numberID.String()
	return nil
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
	return c.request(ctx, http.MethodGet, "/api/direct-download?"+url.Values{"appid": {appID}}.Encode(), nil, dst)
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
		h = &http.Client{Timeout: RequestTimeout}
	}
	hcopy := *h
	priorRedirect := h.CheckRedirect
	hcopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameOrigin(c.BaseURL, req.URL) {
			return fmt.Errorf("redirect to different origin %q", req.URL)
		}
		if priorRedirect != nil {
			return priorRedirect(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	requestCtx, cancel := context.WithTimeout(req.Context(), RequestTimeout)
	defer cancel()
	req = req.WithContext(requestCtx)
	resp, err := hcopy.Do(req)
	if err != nil {
		return redactError(err, c.APIKey)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed (%s)", redact(resp.Status, c.APIKey))
	}
	if out == nil {
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return redactError(err, c.APIKey)
	}
	return nil
}

func sameOrigin(base string, destination *url.URL) bool {
	origin, err := url.Parse(base)
	if err != nil {
		return false
	}
	return strings.EqualFold(origin.Scheme, destination.Scheme) && strings.EqualFold(origin.Host, destination.Host)
}

func redactError(err error, secret string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redact(err.Error(), secret))
}

func redact(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}
