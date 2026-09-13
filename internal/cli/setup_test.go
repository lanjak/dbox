package cli

import (
	"github.com/rufi/dbox/internal/config"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupSavesPrivateConfigurationWithoutPrintingKey(t *testing.T) {
	p := t.TempDir() + "/dbox/config.json"
	var out strings.Builder
	c, err := Setup(strings.NewReader("test-key\n"+t.TempDir()+"\npro\n"), &out, p)
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "test-key" || c.Plan != config.Pro {
		t.Fatalf("bad config %#v", c)
	}
	if strings.Contains(out.String(), "test-key") {
		t.Fatal("setup printed API key")
	}
	if _, err := config.Load(p); err != nil {
		t.Fatal(err)
	}
}

func TestSetupReadsLinesWithoutWaitingForTrailingInput(t *testing.T) {
	d := t.TempDir()
	if _, err := Setup(strings.NewReader("test-key\n"+d+"\nstarter\n"), io.Discard, filepath.Join(d, "config.json")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigEditorChangesOnlySelectedSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	oldDir := t.TempDir()
	c := config.Config{
		APIKey:         "old-key",
		DownloadDir:    oldDir,
		Plan:           config.Pro,
		HandoffTarget:  config.HandoffAccela,
		HandoffLibrary: "/media/DATA/SteamLibrary",
		HandoffCommand: "accela",
	}
	newDir := t.TempDir()
	var out strings.Builder
	got, err := configCommand(strings.NewReader("2\n"+newDir+"\n7\n"), &out, c, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.DownloadDir != newDir {
		t.Fatalf("download directory = %q, want %q", got.DownloadDir, newDir)
	}
	if got.APIKey != c.APIKey || got.Plan != c.Plan || got.HandoffTarget != c.HandoffTarget || got.HandoffLibrary != c.HandoffLibrary || got.HandoffCommand != c.HandoffCommand {
		t.Fatalf("unselected settings changed: got %#v, want %#v", got, c)
	}
	if !strings.Contains(out.String(), "1. API key") || !strings.Contains(out.String(), "7. Done") {
		t.Fatalf("config menu missing: %q", out.String())
	}
}

func TestConfigEditorClearsScreenWhenDone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	c := config.Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: config.Starter}
	var out strings.Builder
	if _, err := configCommand(strings.NewReader("7\n"), &out, c, true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "\033[2J\033[H") {
		t.Fatalf("config exit did not clear screen: %q", out.String())
	}
}
