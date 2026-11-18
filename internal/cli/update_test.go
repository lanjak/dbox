package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewerComparesVersions(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.7", "v0.2.6", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.2.6", "v0.2.6", false},
		{"v0.2.5", "v0.2.6", false},
		{"v0.3.0", "dev", false},
		{"garbage", "v0.2.6", false},
	} {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestOfferUpdateSkipsWhenCurrentOrDeclinedOrOffline(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer s.Close()
	old, oldVer := releasesURL, Version
	defer func() { releasesURL, Version = old, oldVer }()
	releasesURL, Version = s.URL, "v0.2.6"

	var out strings.Builder
	if tag, err := latestVersion(context.Background()); err != nil || tag != "v9.9.9" {
		t.Fatalf("latest %q err %v", tag, err)
	}
	done, err := offerUpdate(context.Background(), strings.NewReader("n\n"), &out)
	if done || err != nil || !strings.Contains(out.String(), "v9.9.9 is available") {
		t.Fatalf("declined: done=%v err=%v out=%q", done, err, out.String())
	}

	Version = "v9.9.9"
	out.Reset()
	if done, _ := offerUpdate(context.Background(), strings.NewReader(""), &out); done || out.Len() != 0 {
		t.Fatalf("up to date but asked: %q", out.String())
	}

	releasesURL = "http://127.0.0.1:1/unreachable"
	Version = "v0.2.6"
	if done, err := offerUpdate(context.Background(), strings.NewReader(""), &out); done || err != nil {
		t.Fatalf("offline check must be silent: %v %v", done, err)
	}
}
