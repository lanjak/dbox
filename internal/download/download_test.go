package download

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilenameRemovesTraversalAndInvalidCharacters(t *testing.T) {
	got := SanitizeFilename("../bad:name?.zip")
	if got != "bad_name_.zip" {
		t.Fatalf("got %q", got)
	}
}
func TestSaveStreamDoesNotOverwriteExistingFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "game.zip")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveStream(d, "game.zip", strings.NewReader("new"), false); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestSaveStreamCollisionDoesNotReplaceFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "game.zip")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveStream(d, "game.zip", strings.NewReader("new"), false); err == nil {
		t.Fatal("expected collision")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "old" {
		t.Fatalf("existing file changed: %q, %v", b, err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken") }
func TestSaveStreamCleansUpFailedTransfer(t *testing.T) {
	d := t.TempDir()
	if _, err := SaveStream(d, "x.zip", failingReader{}, false); err == nil {
		t.Fatal("expected failure")
	}
	entries, _ := os.ReadDir(d)
	if len(entries) != 0 {
		t.Fatalf("temporary files remain: %d", len(entries))
	}
}
