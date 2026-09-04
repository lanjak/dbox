package cli

import (
	"io"
	"strings"
	"testing"
)

func TestRunWithNoArgumentsRequiresConfiguration(t *testing.T) {
	if err := Run(nil, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("expected configuration error")
	}
}
