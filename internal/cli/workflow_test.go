package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/config"
)

type fakeService struct {
	searched   bool
	downloaded string
	fixQuery   string
	fixTags    string
	searchReq  api.SearchRequest
}

func (f *fakeService) Search(context.Context, string) (api.SearchResponse, error) {
	f.searched = true
	return api.SearchResponse{Games: []api.Game{{AppID: "70", Name: "Cyberpunk"}}}, nil
}
func (f *fakeService) SearchWithFilters(_ context.Context, req api.SearchRequest) (api.SearchResponse, error) {
	f.searchReq = req
	return api.SearchResponse{Games: []api.Game{{AppID: "70", Name: "Game"}}}, nil
}
func (f *fakeService) Fixes(_ context.Context, query, tags string) (api.FixesResponse, error) {
	f.fixQuery = query
	f.fixTags = tags
	return api.FixesResponse{Fixes: []api.Fix{{Name: "Fix", File: "fix.zip"}}}, nil
}
func (f *fakeService) DownloadGame(context.Context, string) (string, error) {
	f.downloaded = "game"
	return "game", nil
}
func (f *fakeService) DownloadFix(context.Context, api.Fix) (string, error) {
	f.downloaded = "fix"
	return "fix", nil
}
func TestSearchPrintsResultsAndDownloadsSelection(t *testing.T) {
	f := &fakeService{}
	var out strings.Builder
	err := RunWithService([]string{"search", "Cyberpunk"}, strings.NewReader("1\ny\n"), &out, &out, f, config.Config{DownloadDir: "/tmp", Plan: config.Starter})
	if err != nil {
		t.Fatal(err)
	}
	if !f.searched || f.downloaded != "game" || !strings.Contains(out.String(), "1. Cyberpunk") {
		t.Fatalf("output %q", out.String())
	}
}
func TestDirectDownloadDoesNotPrompt(t *testing.T) {
	f := &fakeService{}
	var out strings.Builder
	err := RunWithService([]string{"download", "70"}, strings.NewReader(""), &out, &out, f, config.Config{DownloadDir: "/tmp", Plan: config.Starter})
	if err != nil || f.downloaded != "game" {
		t.Fatalf("err %v download %q", err, f.downloaded)
	}
}

func TestNoArgumentMenuLoopsUntilExit(t *testing.T) {
	f := &fakeService{}
	var out strings.Builder
	if err := RunWithService(nil, strings.NewReader("6\n"), &out, &out, f, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1. Search games") || !strings.Contains(out.String(), "6. Exit") {
		t.Fatalf("menu output: %s", out.String())
	}
}

func TestMenuPassesFixTags(t *testing.T) {
	f := &fakeService{}
	var out strings.Builder
	if err := RunWithService(nil, strings.NewReader("3\nquery\nonline,offline\n1\nn\n6\n"), &out, &out, f, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if f.fixQuery != "query" || f.fixTags != "online,offline" {
		t.Fatalf("fix filters: query=%q tags=%q", f.fixQuery, f.fixTags)
	}
}

func TestMenuConfirmationLeavesNextCommandForMenu(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	f := &fakeService{}
	var out strings.Builder
	if err := RunWithService(nil, strings.NewReader("1\nGame\n\n1\nn\n5\n"+t.TempDir()+"\nstarter\noff\n\n\n6\n"), &out, &out, f, config.Config{APIKey: "key", DownloadDir: "/tmp", Plan: config.Starter}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "dbox", "config.json")); err != nil {
		t.Fatalf("menu did not return after confirmation: %s", out.String())
	}
}

func TestMenuPassesUserSearchFilters(t *testing.T) {
	f := &fakeService{}
	var out strings.Builder
	if err := RunWithService(nil, strings.NewReader("1\nGame\ndlc,available,protection=online\n1\nn\n6\n"), &out, &out, f, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !f.searchReq.FilterDLC || !f.searchReq.FilterAvailability || f.searchReq.FilterProtection != "online" {
		t.Fatalf("search filters: %#v", f.searchReq)
	}
}

type unreadableInput struct{}

func (unreadableInput) Read([]byte) (int, error) { panic("config view must not read input") }

func TestConfigViewDoesNotReadInput(t *testing.T) {
	var out strings.Builder
	if err := RunWithService([]string{"config"}, unreadableInput{}, &out, &out, &fakeService{}, config.Config{DownloadDir: "/tmp", Plan: config.Starter}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "API key: configured") {
		t.Fatal(out.String())
	}
}
