package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientDoesNotSendAPIKeyAcrossRedirectedHost(t *testing.T) {
	var gotKey string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		io.WriteString(w, `{"games":[]}`)
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		location, _ := url.Parse(other.URL)
		http.Redirect(w, r, location.String()+"/api/search-games", http.StatusFound)
	}))
	defer origin.Close()
	c := New(origin.URL, "test-key", origin.Client())
	_, err := c.Search(context.Background(), SearchRequest{SearchTerm: "x"})
	if err == nil || gotKey != "" {
		t.Fatalf("redirect followed or key leaked: err=%v key=%q", err, gotKey)
	}
}

func TestClientAllowsSameOriginRedirect(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/api/search-games", http.StatusFound)
			return
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("same-origin redirect lost API key")
		}
		io.WriteString(w, `{"games":[]}`)
	}))
	defer s.Close()
	c := New(s.URL, "test-key", s.Client())
	var got SearchResponse
	if err := c.request(context.Background(), http.MethodGet, "/start", nil, &got); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSendsAuthenticatedJSONAndDecodesResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/search-games" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("wrong key header")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"games":[{"appid":"70","name":"Half-Life"}]}`)
	}))
	defer s.Close()
	c := &Client{BaseURL: s.URL, APIKey: "test-key", HTTPClient: s.Client()}
	got, err := c.Search(context.Background(), SearchRequest{SearchTerm: "Half Life", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Games) != 1 || got.Games[0].AppID != "70" {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestFixesEncodesQueryAndDownloadUsesExactHeader(t *testing.T) {
	seen := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/game-fixes" {
			if r.URL.Query().Get("tag") != "online" || r.URL.Query().Get("q") != "A B" {
				t.Errorf("bad query: %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"fixes":[{"id":"f1","file":"fix.zip","name":"Fix"}]}`)
			return
		}
		if r.URL.Path != "/api/direct-download" || r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("bad download request")
		}
		seen++
		io.WriteString(w, "zip")
	}))
	defer s.Close()
	c := &Client{BaseURL: s.URL, APIKey: "test-key", HTTPClient: s.Client()}
	f, err := c.Fixes(context.Background(), "A B", "online")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Fixes) != 1 {
		t.Fatal("fix response not decoded")
	}
	var b strings.Builder
	if err := c.DownloadGame(context.Background(), "70", &b); err != nil {
		t.Fatal(err)
	}
	if b.String() != "zip" || seen != 1 {
		t.Fatalf("download %q, calls %d", b.String(), seen)
	}
}

func TestNonSuccessErrorDoesNotExposeAPIKey(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret response body", 401) }))
	defer s.Close()
	key := "secret-key"
	c := &Client{BaseURL: s.URL, APIKey: key, HTTPClient: s.Client()}
	_, err := c.Search(context.Background(), SearchRequest{SearchTerm: "x"})
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "secret response body") {
		t.Fatalf("bad error: %v", err)
	}
}

func TestNewClientHasBoundedTimeout(t *testing.T) {
	c := New("http://example.com", "test-key", nil)
	if c.HTTPClient == nil || c.HTTPClient.Timeout <= 0 {
		t.Fatal("expected bounded HTTP timeout")
	}
}
