// Package steam finds Steam installations, their library folders and the games
// installed in them. It only reads; handoff writes the files.
package steam

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// appManifestName matches appmanifest_<appid>.acf.
var appManifestName = regexp.MustCompile(`^appmanifest_([0-9]+)\.acf$`)

// kvLine matches one quoted key/value pair in a Valve keyvalues file.
var kvLine = regexp.MustCompile(`^\s*"([^"]+)"\s+"([^"]*)"`)

// libraryPath matches the path entry of a libraryfolders.vdf entry.
var libraryPath = regexp.MustCompile(`^\s*"path"\s+"([^"]+)"`)

// Root returns the Steam installation folder, the one that holds steamapps and
// config. override wins when it is set; otherwise the usual native and Flatpak
// locations are tried.
func Root(override string) (string, error) {
	if o := strings.TrimSpace(override); o != "" {
		root := filepath.Clean(o)
		if !isRoot(root) {
			return "", fmt.Errorf("%s is not a Steam folder (no steamapps inside)", root)
		}
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot find your home folder; set Steam root in dbox config")
	}
	for _, dir := range candidates(home) {
		if isRoot(dir) {
			return dir, nil
		}
	}
	return "", errors.New("no Steam installation found; set Steam root in dbox config")
}

// candidates lists the Steam installation folders in the order they are tried.
func candidates(home string) []string {
	return []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "root"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
	}
}

// isRoot reports whether dir is a Steam installation folder.
func isRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "steamapps"))
	return err == nil && info.IsDir()
}

// Libraries returns the library folders of an installation in Steam's own
// order, so the first one is the default install target. An installation
// without a readable libraryfolders.vdf has its root as the only library.
func Libraries(root string) []string {
	libs := []string{root}
	seen := map[string]bool{filepath.Clean(root): true}
	f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for s.Scan() {
		m := libraryPath.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		dir := filepath.Clean(m[1])
		if seen[dir] || !isLibrary(dir) {
			continue
		}
		seen[dir] = true
		libs = append(libs, dir)
	}
	return libs
}

// isLibrary reports whether dir holds a steamapps folder.
func isLibrary(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "steamapps"))
	return err == nil && info.IsDir()
}

// DefaultLibrary is the library Steam installs into when the user does not pick one.
func DefaultLibrary(root string) string {
	return Libraries(root)[0]
}

// InstallLibrary is the library that holds appID, or the default library when
// the game is not installed yet.
func InstallLibrary(root, appID string) string {
	for _, lib := range Libraries(root) {
		if _, err := os.Stat(filepath.Join(lib, "steamapps", "appmanifest_"+appID+".acf")); err == nil {
			return lib
		}
	}
	return DefaultLibrary(root)
}

// ManifestDir is where Steam keeps depot manifests for one library.
func ManifestDir(library string) string {
	return filepath.Join(library, "steamapps", "depotcache")
}

// PlugInDir is the folder a Steam client mod reads Lua scripts from.
func PlugInDir(root string) string {
	return filepath.Join(root, "config", "stplug-in")
}

// LuaPath is the Lua script of an AppID in a Steam installation.
func LuaPath(root, appID string) string {
	return filepath.Join(PlugInDir(root), appID+".lua")
}

// App is one game that Steam knows about.
type App struct {
	AppID      string
	Name       string
	Library    string
	SizeOnDisk int64
	// Installed is true when Steam reports the files as fully installed.
	Installed bool
	// Managed is true when a Lua script exists for the game, so a client mod
	// installed it. The app layer also sets it for games it downloaded itself.
	Managed bool
}

// InstalledApps reads every appmanifest of an installation. Unreadable or
// malformed manifests are skipped, so a damaged library still lists the rest.
func InstalledApps(root string) []App {
	var apps []App
	for _, lib := range Libraries(root) {
		entries, err := os.ReadDir(filepath.Join(lib, "steamapps"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			m := appManifestName.FindStringSubmatch(e.Name())
			if m == nil || e.IsDir() {
				continue
			}
			a, ok := readManifest(filepath.Join(lib, "steamapps", e.Name()), m[1])
			if !ok {
				continue
			}
			a.Library = lib
			if _, err := os.Stat(LuaPath(root, a.AppID)); err == nil {
				a.Managed = true
			}
			apps = append(apps, a)
		}
	}
	return apps
}

// readManifest reads the fields of one appmanifest file.
func readManifest(path, appID string) (App, bool) {
	f, err := os.Open(path)
	if err != nil {
		return App{}, false
	}
	defer f.Close()
	a := App{AppID: appID}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for s.Scan() {
		m := kvLine.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		switch m[1] {
		case "name":
			a.Name = m[2]
		case "SizeOnDisk":
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
				a.SizeOnDisk = n
			}
		case "StateFlags":
			if n, err := strconv.Atoi(m[2]); err == nil {
				a.Installed = n&4 != 0
			}
		}
	}
	if a.Name == "" {
		return App{}, false
	}
	return a, true
}
