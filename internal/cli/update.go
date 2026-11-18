package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Where dbox looks for new releases and gets its installer. Variables, so tests can point them elsewhere.
var (
	releasesURL  = "https://git.lanjak.net/api/v1/repos/rufi/dbox/releases/latest"
	installerURL = "https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh"
)

// latestVersion asks the release server for the newest release tag.
func latestVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release check: %s", resp.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	return rel.Tag, nil
}

// parseVersion reads "v1.2.3" into numbers. ok is false for "dev" or other text.
func parseVersion(v string) (out [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether latest is a higher version than current.
func newer(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// offerUpdate checks for a newer release and asks to install it. It returns
// true when it ran the installer, so dbox should exit. Any check failure is
// ignored: the update check must never stop the user from working.
func offerUpdate(ctx context.Context, in io.Reader, out io.Writer) (bool, error) {
	latest, err := latestVersion(ctx)
	if err != nil || !newer(latest, Version) {
		return false, nil
	}
	u := newUI(out, out)
	clearScreen(out)
	u.title("Update available")
	fmt.Fprintf(out, "dbox %s is available. You have %s.\n", u.bold(latest), Version)
	fmt.Fprintf(out, "Release notes: https://git.lanjak.net/rufi/dbox/releases/tag/%s\n", latest)
	fmt.Fprintf(out, "\n%s", u.bold("Download and install it now? (y/n): "))
	line, _ := readInputLine(in)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
		return false, nil
	}
	fmt.Fprintln(out)
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		u.fail(out, "cannot find the dbox program file: %v", err)
		return false, waitKey(in, out)
	}
	dir := filepath.Dir(exe)
	if probe, err := os.CreateTemp(dir, ".dbox-update-"); err != nil {
		u.fail(out, "cannot write to %s. Update with:", dir)
		fmt.Fprintf(out, "  curl -fsSL %s | sudo sh\n", installerURL)
		return false, waitKey(in, out)
	} else {
		probe.Close()
		os.Remove(probe.Name())
	}
	script, err := downloadInstaller(ctx)
	if err != nil {
		u.fail(out, "cannot download the installer: %v", err)
		return false, waitKey(in, out)
	}
	defer os.Remove(script)
	// The installer checks the release checksum and replaces the binary in place.
	cmd := exec.CommandContext(ctx, "sh", script, "--version", latest, "--dir", dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, out
	if err := cmd.Run(); err != nil {
		return true, fmt.Errorf("update failed: %w", err)
	}
	fmt.Fprintf(out, "\nStart dbox again to use %s.\n", latest)
	return true, nil
}

func downloadInstaller(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, installerURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("installer download: %s", resp.Status)
	}
	f, err := os.CreateTemp("", "dbox-install-*.sh")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 1<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// waitKey keeps a message on screen until Enter, then dbox carries on to the menu.
func waitKey(in io.Reader, out io.Writer) error {
	fmt.Fprint(out, "\nPress Enter to continue: ")
	_, _ = readInputLine(in)
	return nil
}
