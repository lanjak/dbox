package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// ui writes styled output. Colour and the live spinner are on only when the
// stream is a terminal, so pipes and logs get plain ASCII text.
type ui struct {
	out, errOut io.Writer
	color, live bool
	cur         atomic.Pointer[activity]
	// shown is true once a result or message is printed; the menu then
	// waits for Enter before it clears the screen.
	shown bool
}

func newUI(out, errOut io.Writer) *ui {
	// The legacy Windows console prints ANSI codes as text; Windows Terminal sets WT_SESSION.
	plain := os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || (runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "")
	return &ui{out: out, errOut: errOut, color: !plain && isTTY(out), live: !plain && isTTY(errOut)}
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTerminal(f)
}

func (u *ui) paint(code, s string) string {
	if !u.color || s == "" {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
func (u *ui) bold(s string) string   { return u.paint("1", s) }
func (u *ui) dim(s string) string    { return u.paint("2", s) }
func (u *ui) red(s string) string    { return u.paint("31", s) }
func (u *ui) green(s string) string  { return u.paint("32", s) }
func (u *ui) yellow(s string) string { return u.paint("33", s) }
func (u *ui) cyan(s string) string   { return u.paint("36", s) }

func (u *ui) sym(fancy, plain string) string {
	if u.color {
		return fancy
	}
	return plain
}

func (u *ui) ok(format string, a ...any) {
	u.shown = true
	fmt.Fprintf(u.out, "%s %s\n", u.green(u.sym("✓", "ok:")), fmt.Sprintf(format, a...))
}
func (u *ui) warn(format string, a ...any) {
	u.shown = true
	fmt.Fprintf(u.out, "%s %s\n", u.yellow(u.sym("•", "note:")), fmt.Sprintf(format, a...))
}
func (u *ui) fail(w io.Writer, format string, a ...any) {
	u.shown = true
	fmt.Fprintf(w, "%s %s\n", u.red(u.sym("✗", "error:")), fmt.Sprintf(format, a...))
}
func (u *ui) title(s string) {
	u.shown = true
	fmt.Fprintf(u.out, "%s\n%s\n", u.bold(s), u.dim(strings.Repeat(u.sym("─", "-"), min(utf8.RuneCountInString(s), 60))))
}

// field prints one aligned "label  value" line.
func (u *ui) field(label, value string) {
	fmt.Fprintf(u.out, "  %s %s\n", u.dim(pad(label, 16)), value)
}

// activity is one running download. The spinner line shows bytes received
// and elapsed time; the server builds the ZIP first, so it can stay at 0 for a while.
type activity struct {
	u          *ui
	label      string
	start      time.Time
	bytes      atomic.Int64
	stop, done chan struct{}
}

func (u *ui) start(label string) *activity {
	a := &activity{u: u, label: label, start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	downloads.Add(1)
	u.cur.Store(a)
	if !u.live {
		close(a.done)
		return a
	}
	go func() {
		defer close(a.done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for i := 0; ; i++ {
			state := "waiting for the server to build the package"
			if n := a.bytes.Load(); n > 0 {
				state = humanBytes(n)
			}
			fmt.Fprintf(u.errOut, "\r\033[K%s %s  %s  %s", u.cyan(frames[i%len(frames)]), a.label, u.dim(state), u.dim(humanDuration(time.Since(a.start))))
			select {
			case <-a.stop:
				fmt.Fprint(u.errOut, "\r\033[K")
				return
			case <-t.C:
			}
		}
	}()
	return a
}

// end stops the spinner and returns a short "12.3 MB in 0:42" summary.
func (a *activity) end() string {
	if a.u.cur.CompareAndSwap(a, nil) {
		downloads.Add(-1)
	}
	select {
	case <-a.done:
	default:
		close(a.stop)
		<-a.done
	}
	return fmt.Sprintf("%s in %s", humanBytes(a.bytes.Load()), humanDuration(time.Since(a.start)))
}

func (u *ui) progress(n int64) {
	if a := u.cur.Load(); a != nil {
		a.bytes.Add(n)
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// plural formats a count with the right noun: "1 game", "2 games".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return humanCount(n) + " " + many
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanDuration(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func humanCount(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// pad cuts or pads s to exactly w terminal columns.
func pad(s string, w int) string {
	n := width(s)
	if n <= w {
		return s + strings.Repeat(" ", w-n)
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		if used+runeWidth(r) > w-3 {
			break
		}
		b.WriteRune(r)
		used += runeWidth(r)
	}
	return b.String() + "..." + strings.Repeat(" ", w-3-used)
}

func width(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// runeWidth is the number of terminal columns a character takes: 2 for
// emoji and East Asian wide characters, 0 for combining marks.
// shortcut: covers the common ranges, not every Unicode width rule.
func runeWidth(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200B || r == 0x200D || (r >= 0xFE00 && r <= 0xFE0F):
		return 0
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE4F, r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1FAFF, r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}
