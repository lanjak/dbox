package handoff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InstallSteamZip installs a DepotBox ZIP into a Windows Steam installation.
// The Lua script goes to config/stplug-in and the manifests to
// steamapps/depotcache, where Steam reads them. It does not download game
// content or change Steam's client integration.
func InstallSteamZip(archivePath, steamRoot string) error {
	if steamRoot == "" {
		return errors.New("Steam root is required (set steam_root in dbox config)")
	}
	info, err := os.Stat(filepath.Join(steamRoot, "steam.exe"))
	if err != nil || info.IsDir() {
		return fmt.Errorf("Steam not found at %s (set steam_root to the folder containing steam.exe)", steamRoot)
	}
	entries, appID, err := readSteamBundle(archivePath)
	if err != nil {
		return err
	}
	plugDir := filepath.Join(steamRoot, "config", "stplug-in")
	depotDir := filepath.Join(steamRoot, "steamapps", "depotcache")
	return writeSteamBundle(entries, appID, plugDir, depotDir)
}
