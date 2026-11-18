package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEndpointsUseDocumentedMethodsAndPaths(t *testing.T) {
	var got []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("X-API-Key") != "k" {
			t.Errorf("missing key on %s", r.URL)
		}
		io.WriteString(w, `{}`)
	}))
	defer s.Close()
	c := New(s.URL, "k", s.Client())
	ctx := context.Background()
	var sink strings.Builder
	c.DownloadBatch(ctx, []string{"70", "220"}, true, &sink)
	c.DownloadLua(ctx, "70", &sink)
	c.GameDetails(ctx, "70")
	c.Availability(ctx, []string{"70"})
	c.Usage(ctx)
	c.Stats(ctx)
	c.RequestGame(ctx, "70")
	c.GameRequests(ctx)
	c.Lists(ctx, 2)
	c.List(ctx, "a/b")
	want := []string{
		"GET /api/direct-download/batch?appids=70%2C220&mergeFolders=true",
		"GET /api/direct-lua?appid=70",
		"GET /api/games/70",
		"POST /api/games/batch-availability",
		"GET /api/usage/stats",
		"GET /api/stats",
		"POST /api/game-requests",
		"GET /api/game-requests?limit=25",
		"GET /api/lists/browse?page=2&limit=15",
		"GET /api/lists/a%2Fb",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBatchRejectsMoreThanFiftyAppIDs(t *testing.T) {
	c := New("http://unused", "k", nil)
	if err := c.DownloadBatch(context.Background(), make([]string, MaxBatch+1), false, io.Discard); err == nil {
		t.Fatal("oversized batch accepted")
	}
}

func TestSearchSendsDLCFilterAsString(t *testing.T) {
	var body map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"games":[]}`)
	}))
	defer s.Close()
	c := New(s.URL, "k", s.Client())
	if _, err := c.Search(context.Background(), SearchRequest{SearchTerm: "x", FilterDLC: "exclude", FilterProtection: "require"}); err != nil {
		t.Fatal(err)
	}
	if body["filter_dlc"] != "exclude" || body["filter_protection"] != "require" {
		t.Fatalf("body %#v", body)
	}
}

func TestErrorShowsAPIMessageWithoutKey(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"No manifest for key secret-key"}`)
	}))
	defer s.Close()
	c := New(s.URL, "secret-key", s.Client())
	err := c.DownloadGame(context.Background(), "1", io.Discard)
	if !IsNotFound(err) || !strings.Contains(err.Error(), "No manifest") || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("err %v", err)
	}
}

func TestFailedDownloadWritesNothing(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusNotFound)
	}))
	defer s.Close()
	var b strings.Builder
	if err := New(s.URL, "k", s.Client()).DownloadGame(context.Background(), "1", &b); err == nil || b.Len() != 0 {
		t.Fatalf("err %v wrote %q", err, b.String())
	}
}

func TestTooManyRequestsRetriesAfterStatedDelay(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":"Too Many Requests: API Key limit (60 requests/minute) exceeded. Try again in 0 seconds."}`)
			return
		}
		io.WriteString(w, `{"games":[]}`)
	}))
	defer s.Close()
	start := time.Now()
	if _, err := New(s.URL, "k", s.Client()).Search(context.Background(), SearchRequest{SearchTerm: "x"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || time.Since(start) < time.Second {
		t.Fatalf("calls %d after %v", calls, time.Since(start))
	}
}

func TestFixesFlattensPerGameList(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"games":[{"appid":2622380,"name":"ELDEN RING NIGHTREIGN","fixes":[{"id":"a","downloadName":"E_online.zip","filename":"E_online.zip","size":"32.6 MB","tags":["online"]},{"id":"b","filename":"E_bypass.zip","tags":["bypass"]}]}]}`)
	}))
	defer s.Close()
	got, err := New(s.URL, "k", s.Client()).Fixes(context.Background(), "elden", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Fixes) != 2 || got.Fixes[0].File != "E_online.zip" || got.Fixes[1].File != "E_bypass.zip" || got.Fixes[0].Name != "ELDEN RING NIGHTREIGN" || got.Fixes[0].AppID != "2622380" {
		t.Fatalf("fixes %#v", got.Fixes)
	}
}

func TestNewClientAllowsLongDownloads(t *testing.T) {
	if c := New("http://x", "k", nil); c.HTTPClient.Timeout < DownloadTimeout {
		t.Fatalf("client timeout %v cuts off downloads", c.HTTPClient.Timeout)
	}
}
