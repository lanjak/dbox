package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/api"
	"git.lanjak.net/rufi/dbox/internal/config"
)

func TestFuzzyDistance(t *testing.T) {
	for _, c := range []struct {
		q, name string
		want    int
	}{
		{"freshwoman", "freshwomenseason1", 1},
		{"freshwomen", "freshwomenseason2", 0},
		{"halflife", "halflife2", 0},
		{"freshwoman", "freshstart", 4},
		{"freshwoman", "doom", 3}, // too short to match: stops early at limit+1
	} {
		if got := fuzzyDistance(c.q, c.name, 2); got != c.want {
			t.Errorf("fuzzyDistance(%q, %q) = %d, want %d", c.q, c.name, got, c.want)
		}
	}
}

// fakeDepotBox searches like DepotBox (plain substring match) and serves the
// full game list. listCalls counts game list downloads.
func fakeDepotBox(t *testing.T, listCalls *atomic.Int32, names ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var games []map[string]any
		if r.URL.Path == "/api/gamelist/games" {
			listCalls.Add(1)
			for i, n := range names {
				games = append(games, map[string]any{"appid": 100 + i, "name": n})
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "total": len(games), "games": games, "count": len(games)})
			return
		}
		var req api.SearchRequest
		json.NewDecoder(r.Body).Decode(&req)
		for i, n := range names {
			if strings.Contains(strings.ToLower(n), strings.ToLower(req.SearchTerm)) {
				games = append(games, map[string]any{"appid": 100 + i, "name": n})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"games": games})
	}))
}

func TestSearchFallsBackToCloseMatchesFromGameList(t *testing.T) {
	var listCalls atomic.Int32
	s := fakeDepotBox(t, &listCalls, "FreshWomen - Season 1", "FreshWomen - Season 2", "Fresh Start", "Half-Life")
	defer s.Close()
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: t.TempDir(), Plan: config.Pro}, s.URL, s.Client())
	a.catalog = newCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	for _, q := range []string{"fresh woman", "freshwoman", "fresh women"} {
		r, err := a.Search(context.Background(), q)
		if err != nil || !r.Approximate || len(r.Games) != 2 || !strings.HasPrefix(r.Games[0].Name, "FreshWomen") {
			t.Errorf("%q: %#v err %v", q, r, err)
		}
	}
	if listCalls.Load() != 1 {
		t.Errorf("game list downloaded %d times, want once", listCalls.Load())
	}
	if r, _ := a.Search(context.Background(), "fresh"); r.Approximate || len(r.Games) != 3 {
		t.Errorf("exact search changed: %#v", r)
	}
	if r, _ := a.Search(context.Background(), "zzzzzz"); len(r.Games) != 0 {
		t.Errorf("unrelated term matched: %#v", r.Games)
	}
}

func TestRefreshCatalogSkipsFreshCopyAndRejectsIncompleteList(t *testing.T) {
	var listCalls atomic.Int32
	s := fakeDepotBox(t, &listCalls, "Game")
	defer s.Close()
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: t.TempDir(), Plan: config.Pro}, s.URL, s.Client())
	p := filepath.Join(t.TempDir(), "catalog.json")
	a.catalog = newCatalog(p)
	<-a.catalog.download(context.Background(), a.Client)
	a.RefreshCatalog(context.Background()) // copy is new: no second download
	if listCalls.Load() != 1 {
		t.Fatalf("downloads %d", listCalls.Load())
	}
	os.WriteFile(p, []byte(`{"total":5,"count":2,"games":[{"appid":1,"name":"A"},{"appid":2,"name":"B"}]}`), 0600)
	if _, err := readCatalog(p); err == nil {
		t.Fatal("incomplete list accepted")
	}
}
