package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rufi/dbox/internal/app"
	"github.com/rufi/dbox/internal/cli"
	"github.com/rufi/dbox/internal/config"
)

func TestSearchSelectionAndDirectDownloadAgainstServer(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/search-games" {
			io.WriteString(w, `{"games":[{"appid":"70","name":"Cyberpunk"}]}`)
			return
		}
		if r.URL.Path == "/api/direct-download" {
			io.WriteString(w, "zip")
		}
	}))
	defer s.Close()
	a := app.New(config.Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter}, s.Client())
	a.BaseURL = s.URL
	var out strings.Builder
	if err := cli.RunWithService([]string{"search", "Cyberpunk"}, strings.NewReader("1\ny\n"), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Cyberpunk") {
		t.Fatal(out.String())
	}
}
