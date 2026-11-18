package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"git.lanjak.net/rufi/dbox/internal/config"
	"git.lanjak.net/rufi/dbox/internal/download"
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
	a.Client.BaseURL = s.URL
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

func TestExistingFileCostsNoRequest(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, "zip") }))
	defer s.Close()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "70.zip"), []byte("old"), 0600)
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: d, Plan: config.Starter}, s.URL, s.Client())
	p, err := a.DownloadGame(context.Background(), "70")
	if !errors.Is(err, download.ErrFileExists) || p != filepath.Join(d, "70.zip") || calls != 0 {
		t.Fatalf("p %q err %v calls %d", p, err, calls)
	}
}

func TestDisabledHandoffIgnoresCommandOverride(t *testing.T) {
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: t.TempDir(), Plan: config.Starter, HandoffTarget: config.HandoffNone, HandoffCommand: "/nonexistent/launcher"}, "http://unused", nil)
	if err := a.Handoff("game.zip"); err != nil {
		t.Fatalf("disabled handoff ran: %v", err)
	}
}

func TestProgressCountsSavedBytes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "12345") }))
	defer s.Close()
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: t.TempDir(), Plan: config.Starter}, s.URL, s.Client())
	var n int64
	a.Progress = func(b int64) { n += b }
	if _, err := a.DownloadLua(context.Background(), "70"); err != nil || n != 5 {
		t.Fatalf("err %v bytes %d", err, n)
	}
}

func TestLocalSaveFailureCancelsRequest(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer s.Close()
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0600)
	a := NewWithClient(config.Config{APIKey: "k", DownloadDir: filepath.Join(file, "sub"), Plan: config.Starter}, s.URL, s.Client())
	start := time.Now()
	if _, err := a.DownloadGame(context.Background(), "70"); err == nil || time.Since(start) > time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
}
