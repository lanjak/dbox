package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/app"
	"github.com/rufi/dbox/internal/cli"
	"github.com/rufi/dbox/internal/config"
)

func TestFirstRunSetupAndSearchDownloadAgainstServer(t *testing.T) {
	dir := t.TempDir()
	var gotSearch api.SearchRequest
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("missing API key")
		}
		if r.URL.Path == "/api/search-games" {
			if err := json.NewDecoder(r.Body).Decode(&gotSearch); err != nil {
				t.Fatal(err)
			}
			io.WriteString(w, `{"games":[{"appid":"70","name":"Cyberpunk"}]}`)
			return
		}
		if r.URL.Path == "/api/direct-download" {
			io.WriteString(w, "zip")
			return
		}
		http.NotFound(w, r)
	}))
	defer s.Close()

	configPath := filepath.Join(t.TempDir(), "dbox", "config.json")
	c, err := cli.Setup(strings.NewReader("test-key\n"+dir+"\nstarter\n"), io.Discard, configPath)
	if err != nil {
		t.Fatal(err)
	}
	a := app.NewWithClient(c, s.URL, s.Client())
	var out strings.Builder
	if err := cli.RunWithService([]string{"search", "Cyberpunk"}, strings.NewReader("1\ny\n"), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if gotSearch.SearchTerm != "Cyberpunk" || !strings.Contains(out.String(), "Cyberpunk") {
		t.Fatalf("search workflow: request=%#v output=%q", gotSearch, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "Cyberpunk.zip")); err != nil {
		t.Fatalf("download missing: %v", err)
	}
	if _, err := config.Load(configPath); err != nil {
		t.Fatal(err)
	}
}

func TestFixesDownloadAndDirectAppIDAgainstServer(t *testing.T) {
	dir := t.TempDir()
	var fixQuery, fixTag, downloadedID string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("missing API key")
		}
		switch r.URL.Path {
		case "/api/game-fixes":
			fixQuery, fixTag = r.URL.Query().Get("q"), r.URL.Query().Get("tag")
			io.WriteString(w, `{"fixes":[{"id":"f1","file":"fix.zip","name":"Fix"}]}`)
		case "/api/game-fixes/download":
			if r.URL.Query().Get("id") != "f1" {
				t.Errorf("wrong fix id: %s", r.URL.RawQuery)
			}
			io.WriteString(w, "fix")
		case "/api/direct-download":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			downloadedID = body["appid"]
			io.WriteString(w, "game")
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	a := app.NewWithClient(config.Config{APIKey: "test-key", DownloadDir: dir, Plan: config.Starter}, s.URL, s.Client())
	var out strings.Builder
	if err := cli.RunWithService([]string{"fixes", "A B"}, strings.NewReader("1\ny\n"), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if err := cli.RunWithService([]string{"download", "70"}, strings.NewReader(""), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if fixQuery != "A B" || fixTag != "" || downloadedID != "70" {
		t.Fatalf("requests: query=%q tag=%q appid=%q", fixQuery, fixTag, downloadedID)
	}
	for _, name := range []string{"fix.zip", "70.zip"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestDirectDownloadAcceptsMultipleAppIDsSequentially(t *testing.T) {
	dir := t.TempDir()
	var downloaded []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		downloaded = append(downloaded, body["appid"])
		io.WriteString(w, body["appid"])
	}))
	defer s.Close()
	a := app.NewWithClient(config.Config{APIKey: "test-key", DownloadDir: dir, Plan: config.Starter}, s.URL, s.Client())
	var out strings.Builder
	if err := cli.RunWithService([]string{"download", "70", "71", "72"}, strings.NewReader(""), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if strings.Join(downloaded, ",") != "70,71,72" {
		t.Fatalf("downloads were not sequential: %v", downloaded)
	}
}

func TestSearchSelectionShowsReturnedImageURL(t *testing.T) {
	a := app.NewWithClient(config.Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter}, "http://unused", nil)
	var out strings.Builder
	drm := "DRM note"
	account := "Account note"
	service := &searchService{response: api.SearchResponse{Games: []api.Game{{AppID: "70", Name: "Game", IsDLC: true, DRMNotice: &drm, ExternalUserAccountNotice: &account, HeaderImageURL: "https://example.test/header.jpg"}}}}
	if err := cli.RunWithService([]string{"search", "game"}, strings.NewReader("1\nn\n"), &out, &out, service, a.Config); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"https://example.test/header.jpg", "AppID:", "70", "DLC:                    Yes", "DRM notice:             DRM note", "External account note:  Account note"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q missing from output: %q", want, text)
		}
	}
	if strings.Index(text, "Game\n") > strings.Index(text, "https://example.test/header.jpg") {
		t.Fatalf("game details should precede image URL metadata: %q", text)
	}
}

type searchService struct{ response api.SearchResponse }

func (s *searchService) Search(context.Context, string) (api.SearchResponse, error) {
	return s.response, nil
}
func (s *searchService) Fixes(context.Context, string, string) (api.FixesResponse, error) {
	return api.FixesResponse{}, nil
}
func (s *searchService) DownloadGame(context.Context, string) (string, error) { return "", nil }
func (s *searchService) DownloadFix(context.Context, api.Fix) (string, error) { return "", nil }

func TestMenuConfigChangeUpdatesActiveAppForLaterDownload(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	oldDir, newDir := t.TempDir(), t.TempDir()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/direct-download" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, "game")
	}))
	defer s.Close()
	a := app.NewWithClient(config.Config{APIKey: "test-key", DownloadDir: oldDir, Plan: config.Starter}, s.URL, s.Client())
	input := "5\n2\n" + newDir + "\n3\npro\n4\noff\n7\n2\n70\n6\n"
	var out strings.Builder
	if err := cli.RunWithService(nil, strings.NewReader(input), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if a.Config.DownloadDir != newDir || a.Config.Plan != config.Pro {
		t.Fatalf("active app not updated: %#v", a.Config)
	}
	if _, err := os.Stat(filepath.Join(newDir, "70.zip")); err != nil {
		t.Fatalf("download used old config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "70.zip")); err == nil {
		t.Fatal("download was written to old directory")
	}
}

func TestCLIReportsNonSuccessAPIResponseWithoutSecret(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "test-key response body", http.StatusUnauthorized)
	}))
	defer s.Close()
	a := app.NewWithClient(config.Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter}, s.URL, s.Client())
	err := cli.RunWithService([]string{"search", "x"}, strings.NewReader(""), io.Discard, io.Discard, a, config.Config{})
	if err == nil || strings.Contains(err.Error(), "test-key") || strings.Contains(err.Error(), "response body") {
		t.Fatalf("unsafe API error: %v", err)
	}
}

func TestRealClientHonorsTransportTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		io.WriteString(w, `{"games":[]}`)
	}))
	defer s.Close()
	c := api.New(s.URL, "test-key", s.Client())
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Search(ctx, api.SearchRequest{SearchTerm: "slow"})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout behavior: err=%v elapsed=%v", err, time.Since(start))
	}
}
