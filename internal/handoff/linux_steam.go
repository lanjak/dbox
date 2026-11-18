package handoff

import (
	"git.lanjak.net/rufi/dbox/internal/steam"
)

// InstallLinuxSteamZip installs a DepotBox ZIP into a local Steam installation.
// The Lua script goes to <steam root>/config/stplug-in and the manifests to the
// depotcache of the library that holds the game, or of the default library when
// it is not installed yet. Steam reads both once a client mod such as SLSsteam
// registers the AppID, and the mod creates the appmanifest itself.
//
// steamRoot may be empty: the usual native and Flatpak locations are tried then.
func InstallLinuxSteamZip(archivePath, steamRoot string) error {
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
