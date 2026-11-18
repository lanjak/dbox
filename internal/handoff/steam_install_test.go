package handoff

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSteam builds a Steam installation folder. The only marker is steamapps,
// which every platform has.
func makeSteam(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func makeArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "game.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, content := range entries {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
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

func TestInstallSteamZipPlacesLuaAndManifest(t *testing.T) {
	root := makeSteam(t)
	archive := makeArchive(t, map[string]string{
		"480.lua":                      "addappid(480)",
		"481_123456789012345.manifest": "manifest data",
	})
	if err := InstallSteamZip(archive, root); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(root, "config", "stplug-in", "480.lua"):                          "addappid(480)",
		filepath.Join(root, "steamapps", "depotcache", "481_123456789012345.manifest"): "manifest data",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q, %v", path, got, err)
		}
	}
	if err := InstallSteamZip(archive, root); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
}

func TestInstallSteamZipUsesTheLibraryThatHoldsTheGame(t *testing.T) {
	root := makeSteam(t)
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "steamapps"), 0700); err != nil {
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
	if err := os.WriteFile(filepath.Join(other, "steamapps", "appmanifest_480.acf"), []byte(`"AppState"
{
	"appid"		"480"
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	archive := makeArchive(t, map[string]string{"480.lua": "addappid(480)", "481_9.manifest": "data"})
	if err := InstallSteamZip(archive, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "steamapps", "depotcache", "481_9.manifest")); err != nil {
		t.Fatalf("manifest not in the installed library: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); err != nil {
		t.Fatalf("Lua script: %v", err)
	}
}

func TestInstallSteamZipBacksUpExistingLua(t *testing.T) {
	root := makeSteam(t)
	dir := filepath.Join(root, "config", "stplug-in")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "480.lua"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := InstallSteamZip(makeArchive(t, map[string]string{"480.lua": "updated"}), root); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "480.lua.bak-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %q, %v", backups, err)
	}
	got, err := os.ReadFile(backups[0])
	if err != nil || string(got) != "original" {
		t.Fatalf("backup: %q, %v", got, err)
	}
}

func TestInstallSteamZipRejectsDangerousOrAmbiguousArchiveBeforeWriting(t *testing.T) {
	tests := []map[string]string{
		{"../480.lua": "bad"},
		{"dir/480.lua": "bad"},
		{"480.lua": "a", "481.lua": "b"},
		{"480.lua": "a", "other.txt": "bad"},
		{"480.lua": strings.Repeat("X", 2<<20)},
	}
	for _, entries := range tests {
		root := makeSteam(t)
		if err := InstallSteamZip(makeArchive(t, entries), root); err == nil {
			t.Fatalf("accepted unsafe archive: %v", entries)
		}
		if _, err := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); !os.IsNotExist(err) {
			t.Fatalf("wrote Lua after rejecting archive: %v", err)
		}
	}
}

func TestInstallSteamZipRejectsMissingSteamOrLua(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	archive := makeArchive(t, map[string]string{"123_456.manifest": "data"})
	if err := InstallSteamZip(archive, makeSteam(t)); err == nil {
		t.Fatal("missing Lua accepted")
	}
	if err := InstallSteamZip(makeArchive(t, map[string]string{"480.lua": "a"}), t.TempDir()); err == nil {
		t.Fatal("missing Steam folder accepted")
	}
	if err := InstallSteamZip(makeArchive(t, map[string]string{"480.lua": "a"}), ""); err == nil || !strings.Contains(err.Error(), "no Steam installation found") {
		t.Fatalf("missing installation accepted: %v", err)
	}
}

func TestInstallSteamZipFindsSteamUnderHome(t *testing.T) {
	for _, rel := range []string{
		filepath.Join(".local", "share", "Steam"),
		filepath.Join("Library", "Application Support", "Steam"),
	} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := InstallSteamZip(makeArchive(t, map[string]string{"480.lua": "addappid(480)"}), ""); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}

func TestInstallSteamZipRejectsEmptyLuaBeforeWritingManifest(t *testing.T) {
	root := makeSteam(t)
	err := InstallSteamZip(makeArchive(t, map[string]string{
		"480.lua":          "",
		"481_123.manifest": "data",
	}), root)
	if err == nil {
		t.Fatal("empty Lua accepted")
	}
	if _, e := os.Stat(filepath.Join(root, "steamapps", "depotcache", "481_123.manifest")); !os.IsNotExist(e) {
		t.Fatal("manifest was written despite empty Lua")
	}
}

func TestInstallSteamZipRejectsConflictingManifestWithoutChangingLua(t *testing.T) {
	root := makeSteam(t)
	if err := os.MkdirAll(filepath.Join(root, "steamapps", "depotcache"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "steamapps", "depotcache", "481_123.manifest"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	err := InstallSteamZip(makeArchive(t, map[string]string{"480.lua": "new", "481_123.manifest": "conflict"}), root)
	if err == nil {
		t.Fatal("conflicting manifest accepted")
	}
	if _, e := os.Stat(filepath.Join(root, "config", "stplug-in", "480.lua")); !os.IsNotExist(e) {
		t.Fatal("Lua was written despite manifest conflict")
	}
}
