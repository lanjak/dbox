package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWithNoArgumentsRequiresConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := Run(nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("expected configuration error")
	}
}
