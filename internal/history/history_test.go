package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendsAndLoadsRecentEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store := New(path, 2)
	first := Entry{Kind: "search", Query: "first", At: time.Unix(1, 0).UTC()}
	second := Entry{Kind: "download", AppID: "70", Path: "/tmp/70.zip", At: time.Unix(2, 0).UTC()}
	third := Entry{Kind: "fix-download", Name: "Fix", At: time.Unix(3, 0).UTC()}

	for _, entry := range []Entry{first, second, third} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Kind != second.Kind || entries[1].Kind != third.Kind {
		t.Fatalf("unexpected recent history: %#v", entries)
	}
}
