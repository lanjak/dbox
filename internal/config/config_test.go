package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsUnsupportedPlan(t *testing.T) {
	c := Config{APIKey: "key", DownloadDir: t.TempDir(), Plan: Plan("enterprise")}
	if err := c.Validate(); err == nil {
		t.Fatal("expected unsupported plan error")
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
	want := Config{APIKey: "test-key", DownloadDir: t.TempDir(), Plan: Pro}
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
