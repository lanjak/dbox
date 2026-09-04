package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rufi/dbox/internal/config"
)

func TestAppSearchUsesConfiguredAPI(t *testing.T) {
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
}
