package handoff

import (
	"testing"

	"git.lanjak.net/rufi/dbox/internal/config"
)

func TestCommandForUsesConfiguredOverride(t *testing.T) {
	if got := CommandFor(config.HandoffAccela, "/opt/assella"); got != "/opt/assella" {
		t.Fatalf("got %q", got)
	}
}

func TestCommandForDefaultsToSelectedTarget(t *testing.T) {
	if got := CommandFor(config.HandoffAccela, ""); got != "accela" {
		t.Fatalf("got %q", got)
	}
}
