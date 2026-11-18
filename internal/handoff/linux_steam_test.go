package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSteamRoot builds a Steam installation folder for Linux.
func makeSteamRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstallLinuxSteamZipPlacesLuaInRootAndManifestInLibrary(t *testing.T) {
	root := makeSteamRoot(t)
	archive := makeArchive(t, map[string]string{
		"480.lua":              "addappid(480)",
		"481_1234567.manifest": "manifest data",
	})
	if err := InstallLinuxSteamZip(archive, root); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(root, "config", "stplug-in", "480.lua"):                  "addappid(480)",
		filepath.Join(root, "steamapps", "depotcache", "481_1234567.manifest"): "manifest data",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q, %v", path, got, err)
		}
	}
	if err := InstallLinuxSteamZip(archive, root); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
}

func TestInstallLinuxSteamZipUsesTheLibraryThatHoldsTheGame(t *testing.T) {
	root := makeSteamRoot(t)
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "steamapps", "appmanifest_480.acf"), []byte(`"AppState"
{
	"appid"		"480"
	"name"		"Spacewar"
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(`"libraryfolders"
{
	"0"
	{
		"path"		"`+root+`"
	}
	"1"
	{
		"path"		"`+other+`"
	}
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	archive := makeArchive(t, map[string]string{"480.lua": "addappid(480)", "481_9.manifest": "data"})
	if err := InstallLinuxSteamZip(archive, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "steamapps", "depotcache", "481_9.manifest")); err != nil {
		t.Fatalf("manifest not in the installed library: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "steamapps", "depotcache", "481_9.manifest")); !os.IsNotExist(err) {
		t.Fatalf("manifest written to the default library too: %v", err)
	}
}

func TestInstallLinuxSteamZipFindsSteamUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".local", "share", "Steam")
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := InstallLinuxSteamZip(makeArchive(t, map[string]string{"480.lua": "addappid(480)"}), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallLinuxSteamZipRejectsMissingSteamOrBadArchive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := InstallLinuxSteamZip(makeArchive(t, map[string]string{"480.lua": "a"}), "")
	if err == nil || !strings.Contains(err.Error(), "no Steam installation found") {
		t.Fatalf("missing Steam accepted: %v", err)
	}
	if err := InstallLinuxSteamZip(makeArchive(t, map[string]string{"480.lua": "a"}), t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a Steam folder") {
		t.Fatalf("plain folder accepted: %v", err)
	}
	root := makeSteamRoot(t)
	if err := InstallLinuxSteamZip(makeArchive(t, map[string]string{"../480.lua": "a"}), root); err == nil {
		t.Fatal("unsafe archive accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); !os.IsNotExist(err) {
		t.Fatal("Lua written after rejecting archive")
	}
}
