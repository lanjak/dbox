package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.lanjak.net/rufi/dbox/internal/ratelimit"
)

type Client struct {
	BaseURL, APIKey string
	HTTPClient      *http.Client
	Limiter         *ratelimit.Limiter
}

const (
	// RequestTimeout bounds JSON calls.
	RequestTimeout = 30 * time.Second
	// DownloadTimeout bounds file calls. The server builds the package while
	// the connection is open; the API documentation asks for 10-15 minutes.
	DownloadTimeout = 15 * time.Minute
	// MaxBatch is the largest number of AppIDs that one batch download takes.
	MaxBatch = 50
	// maxRetryWait caps how long a 429 response makes the client wait.
	maxRetryWait = 65 * time.Second
)

func New(baseURL, apiKey string, h *http.Client) *Client {
	if h == nil {
		h = &http.Client{}
	}
	if h.Timeout == 0 {
		h.Timeout = DownloadTimeout
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, HTTPClient: h}
}

// Error is a non-2xx API response. Message is the API's own "error" text,
// with the API key removed.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("DepotBox API error %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("DepotBox API request failed (%d %s)", e.Status, http.StatusText(e.Status))
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// AppID accepts both the numeric and the string AppID forms that the API returns.
type AppID string

func (a *AppID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = AppID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("invalid appid: %w", err)
	}
	*a = AppID(n.String())
	return nil
}

type SearchRequest struct {
	SearchTerm string `json:"searchTerm"`
	Limit      int    `json:"limit,omitempty"`
	// FilterDLC is "only" or "exclude".
	FilterDLC string `json:"filter_dlc,omitempty"`
	// FilterProtection is "require" or "exclude".
	FilterProtection   string `json:"filter_protection,omitempty"`
	FilterAvailability bool   `json:"filter_availability,omitempty"`
}
type Game struct {
	AppID                     AppID   `json:"appid"`
	Name                      string  `json:"name"`
	IsDLC                     bool    `json:"is_dlc,omitempty"`
	DRMNotice                 *string `json:"drm_notice"`
	ExternalUserAccountNotice *string `json:"ext_user_account_notice"`
	HeaderImageURL            string  `json:"header_image_url,omitempty"`
	Description               string  `json:"description,omitempty"`
	Available                 bool    `json:"available,omitempty"`
}
type SearchResponse struct {
	Games []Game `json:"games"`
	// Approximate is true when no name contained the term and Games holds close matches.
	Approximate bool `json:"-"`
}

type GameDetails struct {
	Game
	Availability map[string]bool `json:"availability"`
	DLCs         []Game          `json:"dlcs"`
}

// Available reports whether any source has the game.
func (d GameDetails) Available() bool {
	for _, ok := range d.Availability {
		if ok {
			return true
		}
	}
	return false
}

// Fix is one downloadable fix, flattened from the per-game list that the API returns.
type Fix struct {
	ID    string   `json:"id"`
	File  string   `json:"file"`
	Name  string   `json:"name"`
	AppID string   `json:"appid,omitempty"`
	Size  string   `json:"size,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}
type FixesResponse struct {
	Fixes []Fix `json:"fixes"`
}

type Availability struct {
	AppID     AppID `json:"appid"`
	Available bool  `json:"available"`
}

type Usage struct {
	Stats struct {
		AllTime int `json:"allTime"`
		Last24h int `json:"last24h"`
		Last7d  int `json:"last7d"`
		Last30d int `json:"last30d"`
	} `json:"stats"`
	TopAppIDs []struct {
		AppID AppID `json:"appid"`
		Count int   `json:"request_count"`
	} `json:"top_appids"`
	Recent []struct {
		AppID AppID     `json:"appid"`
		At    time.Time `json:"download_date"`
	} `json:"recent_requests"`
}

type ServiceStats struct {
	TrackedGames   int `json:"tracked_games_db1"`
	Downloads24h   int `json:"total_downloads_last_24h"`
	APIRequests24h int `json:"api_requests_last_24h"`
	MostPopular    struct {
		AppID     AppID  `json:"appid"`
		Name      string `json:"name"`
		Downloads int    `json:"downloads"`
	} `json:"most_popular_depot_last_24h"`
	PublicLists int `json:"public_lists_count"`
}

type GameRequest struct {
	ID              int       `json:"id"`
	AppID           AppID     `json:"appid"`
	GameName        string    `json:"gameName"`
	Status          string    `json:"status"`
	RejectionReason *string   `json:"rejectionReason"`
	CreatedAt       time.Time `json:"createdAt"`
	Message         string    `json:"message"`
}

type ListSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Likes       int    `json:"likes"`
	Downloads   int    `json:"download_count"`
	Creator     string `json:"creator_username"`
	DepotCount  int    `json:"depot_count"`
}
type ListsPage struct {
	Pagination struct {
		CurrentPage int `json:"currentPage"`
		TotalPages  int `json:"totalPages"`
		TotalLists  int `json:"totalLists"`
	} `json:"pagination"`
	Lists []ListSummary `json:"lists"`
}
type List struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Likes       int    `json:"likes"`
	Creator     string `json:"creator_username"`
	Games       []Game `json:"games"`
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
	var raw struct {
		Games []struct {
			AppID AppID  `json:"appid"`
			Name  string `json:"name"`
			Fixes []struct {
				ID           string   `json:"id"`
				DownloadName string   `json:"downloadName"`
				Filename     string   `json:"filename"`
				Size         string   `json:"size"`
				Tags         []string `json:"tags"`
			} `json:"fixes"`
		} `json:"games"`
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &raw, RequestTimeout); err != nil {
		return FixesResponse{}, err
	}
	var out FixesResponse
	for _, g := range raw.Games {
		for _, f := range g.Fixes {
			file := f.DownloadName
			if file == "" {
				file = f.Filename
			}
			out.Fixes = append(out.Fixes, Fix{ID: f.ID, File: file, Name: g.Name, AppID: string(g.AppID), Size: f.Size, Tags: f.Tags})
		}
	}
	return out, nil
}

func (c *Client) GameDetails(ctx context.Context, appID string) (GameDetails, error) {
	var out struct {
		Data GameDetails `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "/api/games/"+url.PathEscape(appID), nil, &out, RequestTimeout)
	return out.Data, err
}

// Availability checks up to 100 AppIDs in one request.
func (c *Client) Availability(ctx context.Context, appIDs []string) ([]Availability, error) {
	var out struct {
		Results []Availability `json:"results"`
	}
	err := c.json(ctx, http.MethodPost, "/api/games/batch-availability", map[string][]string{"appids": appIDs}, &out)
	return out.Results, err
}

func (c *Client) Usage(ctx context.Context) (Usage, error) {
	var out Usage
	err := c.request(ctx, http.MethodGet, "/api/usage/stats", nil, &out, RequestTimeout)
	return out, err
}

func (c *Client) Stats(ctx context.Context) (ServiceStats, error) {
	var out struct {
		Data ServiceStats `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "/api/stats", nil, &out, RequestTimeout)
	return out.Data, err
}

func (c *Client) RequestGame(ctx context.Context, appID string) (GameRequest, error) {
	var out GameRequest
	err := c.json(ctx, http.MethodPost, "/api/game-requests", map[string]string{"appid": appID}, &out)
	return out, err
}

func (c *Client) GameRequests(ctx context.Context) ([]GameRequest, error) {
	var out struct {
		Requests []GameRequest `json:"requests"`
	}
	err := c.request(ctx, http.MethodGet, "/api/game-requests?limit=25", nil, &out, RequestTimeout)
	return out.Requests, err
}

func (c *Client) Lists(ctx context.Context, page int) (ListsPage, error) {
	var out ListsPage
	err := c.request(ctx, http.MethodGet, fmt.Sprintf("/api/lists/browse?page=%d&limit=15", page), nil, &out, RequestTimeout)
	return out, err
}

func (c *Client) List(ctx context.Context, id string) (List, error) {
	var out struct {
		Data List `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "/api/lists/"+url.PathEscape(id), nil, &out, RequestTimeout)
	return out.Data, err
}

func (c *Client) DownloadGame(ctx context.Context, appID string, dst io.Writer) error {
	return c.request(ctx, http.MethodGet, "/api/direct-download?"+url.Values{"appid": {appID}}.Encode(), nil, dst, DownloadTimeout)
}

// DownloadBatch packages up to MaxBatch AppIDs into one ZIP. AppIDs that the
// server cannot build are listed in excluded_manifests.txt inside the ZIP.
func (c *Client) DownloadBatch(ctx context.Context, appIDs []string, merge bool, dst io.Writer) error {
	if len(appIDs) == 0 || len(appIDs) > MaxBatch {
		return fmt.Errorf("a batch takes 1 to %d AppIDs, got %d", MaxBatch, len(appIDs))
	}
	q := url.Values{"appids": {strings.Join(appIDs, ",")}}
	if merge {
		q.Set("mergeFolders", "true")
	}
	return c.request(ctx, http.MethodGet, "/api/direct-download/batch?"+q.Encode(), nil, dst, DownloadTimeout)
}

// DownloadLua fetches only the generated .lua script for an AppID.
func (c *Client) DownloadLua(ctx context.Context, appID string, dst io.Writer) error {
	return c.request(ctx, http.MethodGet, "/api/direct-lua?"+url.Values{"appid": {appID}}.Encode(), nil, dst, DownloadTimeout)
}

func (c *Client) DownloadFix(ctx context.Context, id, file string, dst io.Writer) error {
	q := url.Values{}
	if id != "" {
		q.Set("id", id)
	} else {
		q.Set("file", file)
	}
	return c.request(ctx, http.MethodGet, "/api/game-fixes/download?"+q.Encode(), nil, dst, DownloadTimeout)
}

func (c *Client) json(ctx context.Context, method, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.request(ctx, method, path, b, out, RequestTimeout)
}

var retryIn = regexp.MustCompile(`(?i)try again in (\d+) seconds?`)

// request sends one API call. On a 429 that names a wait time, it waits and
// tries again, at most twice. A file body is written to out only after a 2xx
// status, so an error never ends up inside a saved file.
func (c *Client) request(ctx context.Context, method, path string, body []byte, out any, timeout time.Duration) error {
	for attempt := 0; ; attempt++ {
		err := c.do(ctx, method, path, body, out, timeout)
		var apiErr *Error
		if attempt >= 2 || !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
			return err
		}
		m := retryIn.FindStringSubmatch(apiErr.Message)
		if m == nil {
			return err
		}
		secs, _ := strconv.Atoi(m[1])
		wait := min(time.Duration(secs+1)*time.Second, maxRetryWait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any, timeout time.Duration) error {
	if c.Limiter != nil {
		if err := c.Limiter.Wait(ctx); err != nil {
			return err
		}
	}
	h := c.HTTPClient
	if h == nil {
		h = &http.Client{Timeout: DownloadTimeout}
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
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(requestCtx, method, strings.TrimRight(c.BaseURL, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("User-Agent", "dbox")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hcopy.Do(req)
	if err != nil {
		return redactError(err, c.APIKey)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.apiError(resp)
	}
	if out == nil {
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return redactError(err, c.APIKey)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode API response: %w", redactError(err, c.APIKey))
	}
	return nil
}

// apiError keeps only the JSON "error" field of a failed response. Other body
// text is dropped, so a proxy page or an echoed secret never reaches the user.
func (c *Client) apiError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	msg := strings.TrimSpace(redact(body.Error, c.APIKey))
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	return &Error{Status: resp.StatusCode, Message: msg}
}

func sameOrigin(base string, destination *url.URL) bool {
	origin, err := url.Parse(base)
	if err != nil {
		return false
	}
	return strings.EqualFold(origin.Scheme, destination.Scheme) && strings.EqualFold(origin.Host, destination.Host)
}

func redactError(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err // keep wrapping, so errors.Is still sees context.Canceled
	}
	return fmt.Errorf("%s", redact(err.Error(), secret))
}

func redact(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}
