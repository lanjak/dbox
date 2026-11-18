package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"git.lanjak.net/rufi/dbox/internal/api"
)

type catalogGame struct {
	AppID api.AppID `json:"appid"`
	Name  string    `json:"name"`
	norm  string
}

// catalog is the full DepotBox game list (every AppID and name), saved in the
// user cache folder. The API does not count the download against the rate limit.
type catalog struct {
	path    string
	mu      sync.Mutex
	running chan struct{} // closed when the current download ends
	games   []catalogGame
	err     error // result of the last download
}

func newCatalog(path string) *catalog { return &catalog{path: path} }

// RefreshCatalog downloads a new game list in the background.
func (a *App) RefreshCatalog(ctx context.Context) {
	if a.catalog != nil {
		a.catalog.download(ctx, a.Client)
	}
}

// UpdateGameList downloads a new game list now and returns its size. If a
// download is already running, it waits for that one.
func (a *App) UpdateGameList(ctx context.Context) (int, error) {
	if a.catalog == nil {
		return 0, fmt.Errorf("no cache folder for the game list")
	}
	select {
	case <-a.catalog.download(ctx, a.Client):
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	a.catalog.mu.Lock()
	defer a.catalog.mu.Unlock()
	if a.catalog.err != nil {
		return 0, a.catalog.err
	}
	return len(a.catalog.games), nil
}

// download starts one download at a time and returns a channel that closes when it ends.
func (c *catalog) download(ctx context.Context, client *api.Client) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running != nil {
		return c.running
	}
	done := make(chan struct{})
	c.running = done
	go func() {
		defer close(done)
		games, err := fetchCatalog(ctx, client, c.path)
		c.mu.Lock()
		c.running = nil
		c.err = err
		if err == nil {
			c.games = games
		}
		c.mu.Unlock()
	}()
	return done
}

// fetchCatalog downloads the list to a temporary file, checks that it is
// complete, and only then moves it into place.
func fetchCatalog(ctx context.Context, client *api.Client, path string) ([]catalogGame, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	tmp := path + ".tmp" // a fixed name, so an interrupted download leaves at most one file
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	err = client.GameList(ctx, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	var games []catalogGame
	if err == nil {
		games, err = readCatalog(tmp)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return games, nil
}

func readCatalog(path string) ([]catalogGame, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list struct {
		Total int           `json:"total"`
		Count int           `json:"count"`
		Games []catalogGame `json:"games"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("game list is damaged: %w", err)
	}
	// The API docs: total and count match on a complete list; otherwise retry.
	if list.Total == 0 || list.Total != list.Count || len(list.Games) != list.Count {
		return nil, fmt.Errorf("game list is incomplete (%d of %d games)", len(list.Games), list.Total)
	}
	for i := range list.Games {
		list.Games[i].norm = normalize(list.Games[i].Name)
	}
	return list.Games, nil
}

// load returns the game list. It waits for a running download, reads the
// saved copy, or downloads the list when there is none.
func (c *catalog) load(ctx context.Context, client *api.Client) ([]catalogGame, error) {
	c.mu.Lock()
	games := c.games
	c.mu.Unlock()
	if games != nil {
		return games, nil
	}
	// A saved copy is good enough; a refresh that is running replaces it later.
	if games, err := readCatalog(c.path); err == nil {
		c.mu.Lock()
		if c.games == nil {
			c.games = games
		}
		games = c.games
		c.mu.Unlock()
		return games, nil
	}
	select {
	case <-c.download(ctx, client):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.games == nil {
		return nil, fmt.Errorf("game list is not available: %v", c.err)
	}
	return c.games, nil
}

// closeMatches searches the saved game list when the DepotBox search, which
// only finds names that contain the exact text, finds nothing. It keeps names
// within a small edit distance of the term, so "fresh woman" finds "FreshWomen".
func (a *App) closeMatches(ctx context.Context, req api.SearchRequest) (api.SearchResponse, error) {
	want := normalize(req.SearchTerm)
	if a.catalog == nil || len([]rune(want)) < 4 || isDigits(want) {
		return api.SearchResponse{}, nil
	}
	games, err := a.catalog.load(ctx, a.Client)
	if err != nil {
		return api.SearchResponse{}, nil // the normal "no games found" is still right
	}
	type scored struct {
		g    catalogGame
		dist int
	}
	var found []scored
	limit := max(1, len([]rune(want))/4)
	for _, g := range games {
		if d := fuzzyDistance(want, g.norm, limit); d <= limit {
			found = append(found, scored{g, d})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].dist != found[j].dist {
			return found[i].dist < found[j].dist
		}
		return len(found[i].g.Name) < len(found[j].g.Name)
	})
	out := api.SearchResponse{Approximate: true}
	for i, f := range found {
		if i == max(req.Limit, 25) {
			break
		}
		out.Games = append(out.Games, api.Game{AppID: f.g.AppID, Name: f.g.Name})
	}
	return out, nil
}

// normalize keeps only lower-case letters and digits.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isDigits(s string) bool {
	return strings.TrimFunc(s, unicode.IsDigit) == ""
}

// fuzzyDistance is the smallest number of single-character edits that turn q
// into some part of name (Sellers' approximate substring match). It stops
// early and returns limit+1 when no part of name can be within limit.
func fuzzyDistance(q, name string, limit int) int {
	qr, nr := []rune(q), []rune(name)
	if len(nr)+limit < len(qr) {
		return limit + 1
	}
	prev := make([]int, len(qr)+1)
	for i := range prev {
		prev[i] = i
	}
	best := prev[len(qr)]
	cur := make([]int, len(qr)+1)
	for j := 1; j <= len(nr); j++ {
		cur[0] = 0
		for i := 1; i <= len(qr); i++ {
			cost := 1
			if qr[i-1] == nr[j-1] {
				cost = 0
			}
			cur[i] = min(prev[i-1]+cost, prev[i]+1, cur[i-1]+1)
		}
		best = min(best, cur[len(qr)])
		prev, cur = cur, prev
	}
	return best
}
