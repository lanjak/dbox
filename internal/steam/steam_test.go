package steam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRoot builds a Steam installation folder with one library.
func makeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRootUsesOverrideAndRejectsPlainFolder(t *testing.T) {
	root := makeRoot(t)
	got, err := Root(root)
	if err != nil || got != root {
		t.Fatalf("root %q, %v", got, err)
	}
	if _, err := Root(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a Steam folder") {
		t.Fatalf("plain folder accepted: %v", err)
	}
}

func TestRootFindsSteamUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".local", "share", "Steam")
	if err := os.MkdirAll(filepath.Join(want, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := Root("")
	if err != nil || got != want {
		t.Fatalf("root %q, %v", got, err)
	}
}

func TestRootFindsSteamOnMacOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "Library", "Application Support", "Steam")
	if err := os.MkdirAll(filepath.Join(want, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := Root("")
	if err != nil || got != want {
		t.Fatalf("root %q, %v", got, err)
	}
}

func TestUnescapeVDFHandlesWindowsPaths(t *testing.T) {
	if got := unescapeVDF(`C:\\Program Files (x86)\\Steam`); got != `C:\Program Files (x86)\Steam` {
		t.Fatalf("got %q", got)
	}
	if got := unescapeVDF("/home/rufi/.local/share/Steam"); got != "/home/rufi/.local/share/Steam" {
		t.Fatalf("got %q", got)
	}
}

func TestLibrariesReadsLibraryFoldersAndKeepsOnlyRealLibraries(t *testing.T) {
	root := makeRoot(t)
	second := t.TempDir()
	if err := os.MkdirAll(filepath.Join(second, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"), `"libraryfolders"
{
	"0"
	{
		"path"		"`+root+`"
	}
	"1"
	{
		"path"		"`+second+`"
	}
	"2"
	{
		"path"		"/nonexistent/SteamLibrary"
	}
}
`)
	got := Libraries(root)
	if len(got) != 2 || got[0] != root || got[1] != second {
		t.Fatalf("libraries %q", got)
	}
	if DefaultLibrary(root) != root {
		t.Fatalf("default library %q", DefaultLibrary(root))
	}
}

func TestInstallLibraryPrefersTheLibraryThatHoldsTheGame(t *testing.T) {
	root := makeRoot(t)
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "steamapps"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"), `"libraryfolders"
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
`)
	write(t, filepath.Join(other, "steamapps", "appmanifest_480.acf"), `"AppState"
{
	"appid"		"480"
}
`)
	if got := InstallLibrary(root, "480"); got != other {
		t.Fatalf("installed library %q", got)
	}
	if got := InstallLibrary(root, "70"); got != root {
		t.Fatalf("default library %q", got)
	}
	if got := ManifestDir(other); got != filepath.Join(other, "steamapps", "depotcache") {
		t.Fatalf("manifest dir %q", got)
	}
}

func TestInstalledAppsReadsManifestsAndLuaScripts(t *testing.T) {
	root := makeRoot(t)
	write(t, filepath.Join(root, "steamapps", "appmanifest_480.acf"), `"AppState"
{
	"appid"		"480"
	"name"		"Spacewar"
	"StateFlags"		"4"
	"SizeOnDisk"		"1048576"
}
`)
	write(t, filepath.Join(root, "steamapps", "appmanifest_70.acf"), `"AppState"
{
	"appid"		"70"
	"name"		"Half-Life"
	"StateFlags"		"2"
}
`)
	write(t, filepath.Join(root, "steamapps", "appmanifest_99.acf"), `"AppState"
{
	"appid"		"99"
}
`)
	write(t, filepath.Join(root, "config", "stplug-in", "480.lua"), "addappid(480)\n")

	apps := InstalledApps(root)
	if len(apps) != 2 {
		t.Fatalf("apps %+v", apps)
	}
	byID := map[string]App{}
	for _, a := range apps {
		byID[a.AppID] = a
	}
	if got := byID["480"]; got.Name != "Spacewar" || !got.Installed || !got.Managed || got.SizeOnDisk != 1048576 || got.Library != root {
		t.Fatalf("480: %+v", got)
	}
	if got := byID["70"]; got.Name != "Half-Life" || got.Installed || got.Managed {
		t.Fatalf("70: %+v", got)
	}
}
