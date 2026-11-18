package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	} {
		if got := fuzzyDistance(c.q, c.name); got != c.want {
			t.Errorf("fuzzyDistance(%q, %q) = %d, want %d", c.q, c.name, got, c.want)
		}
	}
}

func TestBroaderTerms(t *testing.T) {
	if got := strings.Join(broaderTerms("Fresh Woman"), "|"); got != "freshwoman|fresh|woman|freshwo" {
		t.Fatalf("terms %q", got)
	}
	if got := strings.Join(broaderTerms("freshwoman"), "|"); got != "freshwo" {
		t.Fatalf("terms %q", got)
	}
}

// fakeCatalog answers like DepotBox: a plain case-insensitive substring match.
func fakeCatalog(t *testing.T, names ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.SearchRequest
		json.NewDecoder(r.Body).Decode(&req)
		var games []map[string]any
		for i, n := range names {
			if strings.Contains(strings.ToLower(n), strings.ToLower(req.SearchTerm)) {
				games = append(games, map[string]any{"appid": 100 + i, "name": n})
			}
		}
		b, _ := json.Marshal(map[string]any{"games": games})
		w.Write(b)
	}))
}

func TestSearchFallsBackToCloseMatches(t *testing.T) {
	s := fakeCatalog(t, "FreshWomen - Season 1", "FreshWomen - Season 2", "Fresh Start", "Half-Life")
	defer s.Close()
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: t.TempDir(), Plan: config.Pro}, s.URL, s.Client())
	for _, q := range []string{"fresh woman", "freshwoman", "fresh women"} {
		r, err := a.Search(context.Background(), q)
		if err != nil || !r.Approximate || len(r.Games) != 2 || !strings.HasPrefix(r.Games[0].Name, "FreshWomen") {
			t.Errorf("%q: %#v err %v", q, r, err)
		}
	}
	r, _ := a.Search(context.Background(), "fresh")
	if r.Approximate || len(r.Games) != 3 {
		t.Errorf("exact search changed: %#v", r)
	}
	if r, _ := a.Search(context.Background(), "zzzzzz"); len(r.Games) != 0 {
		t.Errorf("unrelated term matched: %#v", r.Games)
	}
}
