package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
)

// lineEdit is true when stdin is a Unix terminal. Prompts then get a small
// line editor: arrow keys, Home/End, Backspace/Delete, Ctrl-A/E/U/K/W, and
// Up/Down for earlier entries. Windows consoles have their own line editing.
var lineEdit bool

// termSaved is the terminal state from "stty -g"; termDirty is true while
// dbox has changed it, so Ctrl-C can put it back before exit.
var (
	termSaved string
	termDirty atomic.Bool
)

// stty changes the terminal on stdin. It must get the terminal as its stdin.
func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func saveTerminal() bool {
	s, err := stty("-g")
	if err != nil || s == "" {
		return false
	}
	termSaved = s
	return true
}

func restoreTerminal() {
	if termDirty.Swap(false) && termSaved != "" {
		_, _ = stty(termSaved)
	}
}

func setEcho(on bool) {
	if on {
		restoreTerminal()
		return
	}
	if termSaved == "" && !saveTerminal() {
		return
	}
	if _, err := stty("-echo"); err == nil {
		termDirty.Store(true)
	}
}

var lineHistory []string

// editLine reads one line from the terminal with line editing.
func editLine(r *bufio.Reader, w io.Writer) (string, error) {
	if _, err := stty("-icanon", "-echo", "min", "1", "time", "0"); err != nil {
		line, err := r.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err
	}
	termDirty.Store(true)
	defer restoreTerminal()
	return edit(r, w)
}

// edit is the editor itself, separate from the terminal setup so it can be tested.
// shortcut: it counts one column per rune and does not handle a line that wraps
// past the terminal width; upgrade to width-aware redraw if long input matters.
func edit(r *bufio.Reader, w io.Writer) (string, error) {
	var buf []rune
	pos, hist, draft := 0, len(lineHistory), ""
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			fmt.Fprintln(w)
			return string(buf), err
		}
		old := pos
		switch c {
		case '\r', '\n':
			fmt.Fprint(w, "\n")
			line := string(buf)
			if strings.TrimSpace(line) != "" && (len(lineHistory) == 0 || lineHistory[len(lineHistory)-1] != line) {
				lineHistory = append(lineHistory, line)
			}
			return line, nil
		case 0x7f, 0x08: // Backspace
			if pos > 0 {
				buf = slices.Delete(buf, pos-1, pos)
				pos--
			}
		case 0x04: // Ctrl-D: end of input on an empty line, else delete
			if len(buf) == 0 {
				fmt.Fprintln(w)
				return "", io.EOF
			}
			if pos < len(buf) {
				buf = slices.Delete(buf, pos, pos+1)
			}
		case 0x01: // Ctrl-A
			pos = 0
		case 0x05: // Ctrl-E
			pos = len(buf)
		case 0x02: // Ctrl-B
			pos = max(pos-1, 0)
		case 0x06: // Ctrl-F
			pos = min(pos+1, len(buf))
		case 0x0b: // Ctrl-K
			buf = buf[:pos]
		case 0x15: // Ctrl-U
			buf, pos = slices.Clone(buf[pos:]), 0
		case 0x17: // Ctrl-W
			i := pos
			for i > 0 && buf[i-1] == ' ' {
				i--
			}
			for i > 0 && buf[i-1] != ' ' {
				i--
			}
			buf, pos = slices.Delete(buf, i, pos), i
		case 0x1b:
			// shortcut: a lone Esc waits for the next key; fine for prompts.
			switch key := readEscape(r); {
			case key == "A" && hist > 0: // Up
				if hist == len(lineHistory) {
					draft = string(buf)
				}
				hist--
				buf = []rune(lineHistory[hist])
				pos = len(buf)
			case key == "B" && hist < len(lineHistory): // Down
				hist++
				if hist == len(lineHistory) {
					buf = []rune(draft)
				} else {
					buf = []rune(lineHistory[hist])
				}
				pos = len(buf)
			case strings.HasSuffix(key, "C"):
				pos = min(pos+1, len(buf))
			case strings.HasSuffix(key, "D"):
				pos = max(pos-1, 0)
			case key == "H" || key == "1~" || key == "7~":
				pos = 0
			case key == "F" || key == "4~" || key == "8~":
				pos = len(buf)
			case key == "3~" && pos < len(buf): // Delete
				buf = slices.Delete(buf, pos, pos+1)
			}
		default:
			if c >= 0x20 {
				buf = slices.Insert(buf, pos, c)
				pos++
			}
		}
		// Go back to the start of the input, write it again, clear the rest, then
		// put the cursor where it belongs. ESC[0D moves one column, so skip 0.
		if old > 0 {
			fmt.Fprintf(w, "\033[%dD", old)
		}
		fmt.Fprint(w, string(buf), "\033[K")
		if back := len(buf) - pos; back > 0 {
			fmt.Fprintf(w, "\033[%dD", back)
		}
	}
}

// readEscape reads the rest of an escape sequence after ESC and returns its
// parameters and final byte, for example "D" for Left or "3~" for Delete.
func readEscape(r *bufio.Reader) string {
	b, err := r.ReadByte()
	if err != nil || (b != '[' && b != 'O') {
		return ""
	}
	var seq []byte
	for len(seq) < 16 {
		b, err := r.ReadByte()
		if err != nil {
			return ""
		}
		seq = append(seq, b)
		if b >= 0x40 && b <= 0x7e {
			break
		}
	}
	return string(seq)
}
