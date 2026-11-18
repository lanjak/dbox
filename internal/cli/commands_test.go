package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/api"
	"git.lanjak.net/rufi/dbox/internal/config"
	"git.lanjak.net/rufi/dbox/internal/download"
)

func TestParseArgsSplitsFlagsAndTerms(t *testing.T) {
	flags, pos, err := parseArgs([]string{"half", "--limit", "5", "--no-dlc", "life", "--tag=online"}, map[string]bool{"limit": true, "no-dlc": false, "tag": true})
	if err != nil || flags["limit"] != "5" || flags["tag"] != "online" || strings.Join(pos, " ") != "half life" {
		t.Fatalf("flags %v pos %v err %v", flags, pos, err)
	}
	if _, _, err := parseArgs([]string{"--bogus"}, map[string]bool{}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestSearchFlagsMapToAPIValues(t *testing.T) {
	f := &fakeService{}
	if err := RunWithService([]string{"search", "--no-dlc", "--drm", "--available", "--limit", "5", "half", "life"}, strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, f, config.Config{}); err != nil {
		t.Fatal(err)
	}
	r := f.searchReq
	if r.SearchTerm != "half life" || r.FilterDLC != "exclude" || r.FilterProtection != "require" || !r.FilterAvailability || r.Limit != 5 {
		t.Fatalf("request %#v", r)
	}
}

func TestDownloadRejectsNonNumericAppID(t *testing.T) {
	f := &fakeService{}
	if err := RunWithService([]string{"download", "../70"}, strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, f, config.Config{}); err == nil || f.downloaded != "" {
		t.Fatalf("bad AppID accepted: %v", err)
	}
}

type failingSearch struct{ fakeService }

func (f *failingSearch) SearchWithFilters(context.Context, api.SearchRequest) (api.SearchResponse, error) {
	return api.SearchResponse{}, errors.New("network down")
}

func TestMenuReportsErrorAndKeepsRunning(t *testing.T) {
	var out strings.Builder
	if err := RunWithService(nil, strings.NewReader("1\nx\n\nq\n"), &out, &out, &failingSearch{}, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "network down") || strings.Count(out.String(), "q. Quit") != 2 {
		t.Fatalf("menu did not continue after error: %q", out.String())
	}
}

type existingIDService struct{ fakeService }

func (s *existingIDService) DownloadGame(context.Context, string) (string, error) {
	return "/tmp/70.zip", download.ErrFileExists
}

func TestDownloadOfExistingFileIsNotAFailure(t *testing.T) {
	var out strings.Builder
	if err := RunWithService([]string{"download", "70"}, strings.NewReader(""), &out, &out, &existingIDService{}, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Already downloaded: /tmp/70.zip") {
		t.Fatal(out.String())
	}
}

func TestFixDownloadIsNotHandedOff(t *testing.T) {
	s := &existingDownloadService{enabled: true}
	if err := RunWithService([]string{"fixes"}, strings.NewReader("1\ny\n"), &strings.Builder{}, &strings.Builder{}, s, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if s.downloaded != "fix" || s.handedOff {
		t.Fatalf("downloaded %q handed off %v", s.downloaded, s.handedOff)
	}
}

func TestRunDoesNotOverwriteUnreadableConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "dbox", "config.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("{broken"), 0600)
	err := Run([]string{"usage"}, strings.NewReader("new-key\n/tmp\npro\n"), &strings.Builder{}, &strings.Builder{})
	if b, _ := os.ReadFile(p); err == nil || string(b) != "{broken" {
		t.Fatalf("err %v, config now %q", err, b)
	}
}

func TestHelpAndVersionNeedNoConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "none"))
	var out strings.Builder
	if err := Run([]string{"help"}, strings.NewReader(""), &out, &out); err != nil || !strings.Contains(out.String(), "dbox download") {
		t.Fatalf("help: %v %q", err, out.String())
	}
}

func TestBlankSelectionGoesBack(t *testing.T) {
	f := &fakeService{}
	if err := RunWithService([]string{"search", "x"}, strings.NewReader("\n"), &strings.Builder{}, &strings.Builder{}, f, config.Config{}); err != nil || f.downloaded != "" {
		t.Fatalf("err %v downloaded %q", err, f.downloaded)
	}
}
