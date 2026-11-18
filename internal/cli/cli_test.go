package cli

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"git.lanjak.net/rufi/dbox/internal/config"
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

func testSession(out *strings.Builder, c config.Config) *session {
	return &session{ctx: context.Background(), out: out, errOut: out, c: c, ui: newUI(out, out)}
}
