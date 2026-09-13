package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestClearScreenWritesTerminalClearSequence(t *testing.T) {
	var out strings.Builder
	clearScreen(&out)
	if got, want := out.String(), "\033[2J\033[H"; got != want {
		t.Fatalf("clear sequence = %q, want %q", got, want)
	}
}

func TestRunWithNoArgumentsRequiresConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := Run(nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("expected configuration error")
	}
}
