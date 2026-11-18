package cli

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestEditHandlesCursorKeys(t *testing.T) {
	cases := map[string]string{
		"abc\x1b[D\x1b[DX\n":     "aXbc",  // Left twice, insert
		"hello\x7f\x7f\n":        "hel",   // Backspace
		"foo\x01X\x05Y\n":        "XfooY", // Ctrl-A, Ctrl-E
		"abc\x1b[H\x1b[3~\n":     "bc",    // Home, Delete
		"abc\x1bOH\x1b[F!\n":     "abc!",  // SS3 Home, End
		"one two\x17\n":          "one ",  // Ctrl-W
		"abc\x1b[D\x0b\n":        "ab",    // Ctrl-K
		"x\x1b[D\x1b[D\x1b[DY\n": "Yx",    // Left stops at the start
	}
	for in, want := range cases {
		lineHistory = nil
		got, err := edit(bufio.NewReader(strings.NewReader(in)), io.Discard)
		if err != nil || got != want {
			t.Errorf("edit(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestEditRecallsHistoryWithUpAndDown(t *testing.T) {
	lineHistory = []string{"first", "second"}
	r := bufio.NewReader(strings.NewReader("\x1b[A\x1b[A\n" + "dra\x1b[A\x1b[B\n"))
	if got, _ := edit(r, io.Discard); got != "first" {
		t.Fatalf("up twice = %q", got)
	}
	if got, _ := edit(r, io.Discard); got != "dra" {
		t.Fatalf("up then down = %q, want the draft back", got)
	}
}

func TestEditEndsOnCtrlDWhenEmpty(t *testing.T) {
	if _, err := edit(bufio.NewReader(strings.NewReader("\x04")), io.Discard); err != io.EOF {
		t.Fatalf("err %v", err)
	}
}

func TestAppIDFromAcceptsStoreLinks(t *testing.T) {
	for in, want := range map[string]string{
		"70": "70",
		"https://store.steampowered.com/app/1350650/Some_Game/": "1350650",
		"store.steampowered.com/app/1350650":                    "1350650",
		"https://steamdb.info/app/220/":                         "220",
	} {
		if got := appIDFrom(in); got != want {
			t.Errorf("appIDFrom(%q) = %q, want %q", in, got, want)
		}
	}
}
