package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
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

// pad cuts or pads s to exactly w runes.
func pad(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n > w {
		r := []rune(s)
		return string(r[:w-3]) + "..."
	}
	return s + strings.Repeat(" ", w-n)
}
