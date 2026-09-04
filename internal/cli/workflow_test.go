package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/config"
)

type fakeService struct {
	searched   bool
	downloaded string
}

func (f *fakeService) Search(context.Context, string) (api.SearchResponse, error) {
	f.searched = true
	return api.SearchResponse{Games: []api.Game{{AppID: "70", Name: "Cyberpunk"}}}, nil
}
func (f *fakeService) Fixes(context.Context, string, string) (api.FixesResponse, error) {
	return api.FixesResponse{}, nil
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
