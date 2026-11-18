package integration

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/app"
	"git.lanjak.net/rufi/dbox/internal/cli"
	"git.lanjak.net/rufi/dbox/internal/config"
)

// writeBundle builds a DepotBox-style ZIP with one Lua script and one manifest.
func writeBundle(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Spacewar.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, content := range map[string]string{
		"480.lua":              "addappid(480, 1, \"key\")\n",
		"481_1234567.manifest": "manifest",
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestInstallCommandWritesLinuxSteamHandoff runs the real CLI against a fake
// Steam installation, so the file layout that SLSsteam reads is checked end to
// end, not only in the handoff package.
func TestInstallCommandWritesLinuxSteamHandoff(t *testing.T) {
	steamRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(steamRoot, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	if err := os.MkdirAll(filepath.Join(library, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "steamapps", "appmanifest_480.acf"), []byte("\"AppState\"\n{\n\t\"appid\"\t\t\"480\"\n\t\"name\"\t\t\"Spacewar\"\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"), []byte("\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\""+steamRoot+"\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\""+library+"\"\n\t}\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{
		APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter,
		HandoffTarget: config.HandoffLinuxSteam, SteamRoot: steamRoot,
	}
	a := app.NewWithClient(c, "http://unused", nil)
	var out strings.Builder
	if err := cli.RunWithService([]string{"install", writeBundle(t)}, strings.NewReader(""), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Installed:") {
		t.Fatalf("output %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(steamRoot, "config", "stplug-in", "480.lua")); err != nil {
		t.Fatalf("Lua script: %v", err)
	}
	if _, err := os.Stat(filepath.Join(library, "steamapps", "depotcache", "481_1234567.manifest")); err != nil {
		t.Fatalf("manifest in the installed library: %v", err)
	}
	// The library view reads Steam's own files, so the install shows up there.
	if err := cli.RunWithService([]string{"installed"}, strings.NewReader(""), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Spacewar") || !strings.Contains(out.String(), "managed") {
		t.Fatalf("installed view %q", out.String())
	}
}
