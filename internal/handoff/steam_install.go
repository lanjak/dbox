package handoff

import (
	"git.lanjak.net/rufi/dbox/internal/steam"
)

// InstallSteamZip installs a DepotBox ZIP into the local Steam installation of
// Windows, macOS or Linux. The Lua script goes to config/stplug-in and the
// manifests to the steamapps/depotcache of the library that holds the game, or
// of the default library. It does not download game content or change Steam
// itself; a client mod such as SLSsteam registers the AppID on Linux.
//
// steamRoot may be empty: the usual locations of the three platforms are tried.
func InstallSteamZip(archivePath, steamRoot string) error {
	root, err := steam.Root(steamRoot)
	if err != nil {
		return err
	}
	entries, appID, err := readSteamBundle(archivePath)
	if err != nil {
		return err
	}
	library := steam.InstallLibrary(root, appID)
	return writeSteamBundle(entries, appID, steam.PlugInDir(root), steam.ManifestDir(library))
}
