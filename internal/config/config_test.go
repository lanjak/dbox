package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveRejectsExistingSymlinkOnUnix(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix symlink protection")
	}
	d := t.TempDir()
	target := filepath.Join(d, "target.json")
	path := filepath.Join(d, "config.json")
	if err := os.WriteFile(target, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c := Config{APIKey: "key", DownloadDir: d, Plan: Starter}
	if err := Save(path, c); err == nil {
		t.Fatal("expected symlink save to fail")
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "original\n" {
		t.Fatalf("symlink target changed: %q, %v", b, err)
	}
}

func TestValidateRejectsUnsupportedPlan(t *testing.T) {
	c := Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: Plan("enterprise")}
	if err := c.Validate(); err == nil {
		t.Fatal("expected unsupported plan error")
	}
}

func TestValidateRejectsUnsupportedHandoffTarget(t *testing.T) {
	c := Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: Starter, HandoffTarget: HandoffTarget("other")}
	if err := c.Validate(); err == nil {
		t.Fatal("expected unsupported handoff target error")
	}
}

func TestValidateWindowsSteamRequiresSteamRoot(t *testing.T) {
	c := Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: Pro, HandoffTarget: HandoffTarget("windows-steam")}
	if err := c.Validate(); err == nil {
		t.Fatal("missing Steam root accepted")
	}
	c.SteamRoot = `C:\Program Files (x86)\Steam`
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsEmptyKeyAndDirectory(t *testing.T) {
	for _, c := range []Config{{DownloadDir: t.TempDir(), Plan: Starter}, {APIKey: "key", Plan: Starter}} {
		if err := c.Validate(); err == nil {
			t.Fatal("expected validation error")
		}
	}
}

func TestSaveLoadRoundTripCreatesParentAndPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: Pro, HandoffTarget: HandoffAccela, HandoffLibrary: "/media/DATA/SteamLibrary", HandoffCommand: "/usr/bin/accela"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o, want 0600", info.Mode().Perm())
	}
}
