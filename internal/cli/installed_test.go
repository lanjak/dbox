package cli

import (
	"errors"
	"strings"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/config"
	"git.lanjak.net/rufi/dbox/internal/steam"
)

type installedService struct {
	fakeService
	apps []steam.App
	err  error
}

func (s *installedService) InstalledApps() ([]steam.App, error) { return s.apps, s.err }

func TestInstalledListsGamesAndMarksHandedOffOnes(t *testing.T) {
	s := &installedService{apps: []steam.App{
		{AppID: "70", Name: "Half-Life", Library: "/steam", SizeOnDisk: 1 << 20, Installed: true},
		{AppID: "480", Name: "Spacewar", Library: "/data/SteamLibrary", SizeOnDisk: 2 << 20, Installed: true, Managed: true},
		{AppID: "620", Name: "Portal 2", Library: "/steam"},
	}}
	var out strings.Builder
	if err := RunWithService([]string{"installed"}, strings.NewReader(""), &out, &out, s, config.Config{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Half-Life", "Spacewar", "Portal 2", "managed", "not installed", "3 games, 1 managed by dbox or a client mod"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	// Sorted by name: Half-Life, Portal 2, Spacewar.
	if strings.Index(text, "Half-Life") > strings.Index(text, "Portal 2") || strings.Index(text, "Portal 2") > strings.Index(text, "Spacewar") {
		t.Fatalf("not sorted by name: %q", text)
	}
}

func TestInstalledReportsAMissingSteamInstallation(t *testing.T) {
	s := &installedService{err: errors.New("no Steam installation found; set Steam root in dbox config")}
	err := RunWithService([]string{"installed"}, strings.NewReader(""), &strings.Builder{}, &strings.Builder{}, s, config.Config{})
	if err == nil || !strings.Contains(err.Error(), "no Steam installation found") {
		t.Fatalf("err %v", err)
	}
}

func TestInstalledIsUnavailableWithoutTheSteamReader(t *testing.T) {
	var out strings.Builder
	if err := RunWithService([]string{"installed"}, strings.NewReader(""), &out, &out, &fakeService{}, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Installed games are unavailable") {
		t.Fatalf("out %q", out.String())
	}
}

func TestMenuListsInstalledGames(t *testing.T) {
	s := &installedService{apps: []steam.App{{AppID: "480", Name: "Spacewar", Library: "/steam", Installed: true}}}
	var out strings.Builder
	// i: installed games, Enter to return to the menu, q: quit.
	if err := RunWithService(nil, strings.NewReader("i\n\nq\n"), &out, &out, s, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Spacewar") || !strings.Contains(out.String(), "i. Installed games") {
		t.Fatalf("menu output %q", out.String())
	}
}
