package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/app"
	"git.lanjak.net/rufi/dbox/internal/cli"
	"git.lanjak.net/rufi/dbox/internal/config"
)

// TestInstalledCommandReadsRealSteamFiles runs the CLI against a fake Steam
// installation, so the library view is checked through the same path a user
// runs, not only in the steam package.
func TestInstalledCommandReadsRealSteamFiles(t *testing.T) {
	root := t.TempDir()
	library := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "steamapps"), filepath.Join(library, "steamapps"), filepath.Join(root, "config", "stplug-in")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "steamapps", "libraryfolders.vdf"):     "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"" + root + "\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"" + library + "\"\n\t}\n}\n",
		filepath.Join(root, "steamapps", "appmanifest_70.acf"):     "\"AppState\"\n{\n\t\"appid\"\t\t\"70\"\n\t\"name\"\t\t\"Half-Life\"\n\t\"StateFlags\"\t\t\"4\"\n\t\"SizeOnDisk\"\t\t\"1048576\"\n}\n",
		filepath.Join(library, "steamapps", "appmanifest_480.acf"): "\"AppState\"\n{\n\t\"appid\"\t\t\"480\"\n\t\"name\"\t\t\"Spacewar\"\n\t\"StateFlags\"\t\t\"4\"\n}\n",
		filepath.Join(root, "config", "stplug-in", "480.lua"):      "addappid(480)\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := config.Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: config.Starter, SteamRoot: root}
	a := app.NewWithClient(c, "http://unused", nil)
	var out strings.Builder
	if err := cli.RunWithService([]string{"installed"}, strings.NewReader(""), &out, &out, a, a.Config); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Half-Life", "Spacewar", library, "managed", "2 games, 1 managed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}
