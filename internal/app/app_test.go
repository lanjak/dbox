package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rufi/dbox/internal/config"
)

func TestAppSearchUsesConfiguredAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Error("missing key")
		}
		io.WriteString(w, `{"games":[{"appid":"70","name":"Game"}]}`)
	}))
	defer s.Close()
	a := New(config.Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter}, s.Client())
	a.BaseURL = s.URL
	got, err := a.Search(context.Background(), "Game")
	if err != nil || len(got.Games) != 1 || !called {
		t.Fatalf("got %#v err %v", got, err)
	}
	entries, err := a.HistoryEntries()
	if err != nil || len(entries) != 1 || entries[0].Kind != "search" || entries[0].Query != "Game" {
		t.Fatalf("history %#v err %v", entries, err)
	}
}

func TestNamedGameDownloadUsesSanitizedName(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "zip") }))
	defer s.Close()
	d := t.TempDir()
	a := NewWithClient(config.Config{APIKey: "test-key", DownloadDir: d, Plan: config.Starter}, s.URL, s.Client())
	p, err := a.DownloadGameNamed(context.Background(), "70", "My/Game: Deluxe")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "Game_ Deluxe.zip" {
		t.Fatalf("path %q", p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
