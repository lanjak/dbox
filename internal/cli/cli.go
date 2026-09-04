package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/app"
	"github.com/rufi/dbox/internal/config"
	"github.com/rufi/dbox/internal/history"
)

type Service interface {
	Search(context.Context, string) (api.SearchResponse, error)
	Fixes(context.Context, string, string) (api.FixesResponse, error)
	DownloadGame(context.Context, string) (string, error)
	DownloadFix(context.Context, api.Fix) (string, error)
}
type filteredSearcher interface {
	SearchWithFilters(context.Context, api.SearchRequest) (api.SearchResponse, error)
}
type namedDownloader interface {
	DownloadGameNamed(context.Context, string, string) (string, error)
}
type configUpdater interface {
	UpdateConfig(config.Config)
}
type historyLister interface {
	HistoryEntries() ([]history.Entry, error)
}

func Run(args []string, in io.Reader, out, errOut io.Writer) error {
	p, err := config.DefaultPath()
	if err != nil {
		return err
	}
	c, err := config.Load(p)
	if err != nil {
		c, err = Setup(in, out, p)
		if err != nil {
			return err
		}
	}
	return RunWithService(args, in, out, errOut, app.New(c, nil), c)
}

func Setup(in io.Reader, out io.Writer, path string) (config.Config, error) {
	fmt.Fprintln(out, "Depotbox setup")
	key, err := readAPIKey(in, out)
	if err != nil {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	dir, err := readLineFrom(in, out, "Download directory: ")
	if err != nil {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	plan, err := readLineFrom(in, out, "Plan (starter/pro): ")
	if err != nil {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	c := config.Config{APIKey: strings.TrimSpace(key), DownloadDir: strings.TrimSpace(dir), Plan: config.Plan(strings.TrimSpace(plan))}
	if err := c.Validate(); err != nil {
		return config.Config{}, err
	}
	if err := os.MkdirAll(c.DownloadDir, 0700); err != nil {
		return config.Config{}, err
	}
	fmt.Fprintln(out, "API key configured")
	return c, config.Save(path, c)
}

func readAPIKey(input io.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "API key: ")
	if f, ok := input.(*os.File); ok && isTerminal(f) {
		_ = exec.Command("stty", "-echo").Run()
		defer func() { _ = exec.Command("stty", "echo").Run(); fmt.Fprintln(out) }()
	}
	return readLineFrom(input, out, "")
}
func isTerminal(f *os.File) bool {
	i, err := f.Stat()
	return err == nil && i.Mode()&os.ModeCharDevice != 0
}
func readLine(r *bufio.Reader, out io.Writer, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(out, prompt)
	}
	s, err := r.ReadString('\n')
	return strings.TrimRight(s, "\r\n"), err
}

func readLineFrom(in io.Reader, out io.Writer, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(out, prompt)
	}
	return readInputLine(in)
}

func RunWithService(args []string, in io.Reader, out, errOut io.Writer, s Service, c config.Config) error {
	ctx := context.Background()
	if len(args) == 0 {
		return menu(ctx, in, out, s, c)
	}
	switch args[0] {
	case "search":
		if len(args) != 2 {
			return errors.New("usage: dbox search <term>")
		}
		r, err := s.Search(ctx, args[1])
		if err != nil {
			return err
		}
		return chooseGame(ctx, in, out, s, r)
	case "download":
		if len(args) < 2 {
			return errors.New("usage: dbox download <appid> [appid ...]")
		}
		return downloadIDs(ctx, args[1:], out, errOut, s)
	case "history":
		if len(args) > 2 {
			return errors.New("usage: dbox history [search-term]")
		}
		return showHistory(out, s, strings.Join(args[1:], " "))
	case "fixes":
		if len(args) > 2 {
			return errors.New("usage: dbox fixes [query]")
		}
		q := ""
		if len(args) == 2 {
			q = args[1]
		}
		r, err := s.Fixes(ctx, q, "")
		if err != nil {
			return err
		}
		return chooseFix(ctx, in, out, s, r)
	case "config":
		if len(args) > 2 {
			return errors.New("usage: dbox config [edit]")
		}
		_, err := configCommand(in, out, c, len(args) == 2 && args[1] == "edit")
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func menu(ctx context.Context, in io.Reader, out io.Writer, s Service, c config.Config) error {
	r := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "1. Search games\n2. Download by AppID\n3. Game fixes\n4. History\n5. Config\n6. Exit")
		choice, err := readLine(r, out, "Select an option: ")
		if err != nil {
			return nil
		}
		switch strings.TrimSpace(choice) {
		case "1":
			term, e := readLine(r, out, "Search term: ")
			if e != nil {
				return e
			}
			req := api.SearchRequest{SearchTerm: strings.TrimSpace(term)}
			filters, e := readLine(r, out, "Filters (dlc, available, protection=<name>, or blank): ")
			if e != nil {
				return e
			}
			applySearchFilters(&req, filters)
			var result api.SearchResponse
			if fs, ok := s.(filteredSearcher); ok {
				result, e = fs.SearchWithFilters(ctx, req)
			} else {
				result, e = s.Search(ctx, req.SearchTerm)
			}
			if e != nil {
				return e
			}
			if e = chooseGame(ctx, r, out, s, result); e != nil {
				return e
			}
		case "2":
			ids, e := readLine(r, out, "AppID(s), separated by spaces or commas: ")
			if e != nil {
				return e
			}
			if e = downloadIDs(ctx, splitIDs(ids), out, out, s); e != nil {
				return e
			}
		case "3":
			q, e := readLine(r, out, "Fix search (blank for all): ")
			if e != nil {
				return e
			}
			tags, e := readLine(r, out, "Fix tags (comma-separated, or blank): ")
			if e != nil {
				return e
			}
			result, e := s.Fixes(ctx, strings.TrimSpace(q), strings.TrimSpace(tags))
			if e != nil {
				return e
			}
			if e = chooseFix(ctx, r, out, s, result); e != nil {
				return e
			}
		case "4":
			if e := showHistory(out, s, ""); e != nil {
				return e
			}
		case "5":
			updated, e := configCommand(r, out, c, true)
			if e != nil {
				return e
			}
			c = updated
			if updater, ok := s.(configUpdater); ok {
				updater.UpdateConfig(c)
			}
		case "6":
			return nil
		default:
			fmt.Fprintln(out, "invalid selection")
		}
	}
}

func applySearchFilters(req *api.SearchRequest, text string) {
	for _, p := range strings.Split(strings.ToLower(strings.TrimSpace(text)), ",") {
		p = strings.TrimSpace(p)
		switch {
		case p == "dlc":
			req.FilterDLC = true
		case p == "available":
			req.FilterAvailability = true
		case strings.HasPrefix(p, "protection="):
			req.FilterProtection = strings.TrimSpace(strings.TrimPrefix(p, "protection="))
		}
	}
}

func configCommand(in io.Reader, out io.Writer, c config.Config, edit bool) (config.Config, error) {
	fmt.Fprintf(out, "Download directory: %s\nPlan: %s\nAPI key: configured\n", c.DownloadDir, c.Plan)
	if !edit {
		return c, nil
	}
	dir, err := readLineFrom(in, out, "New download directory: ")
	if err != nil {
		return c, err
	}
	plan, err := readLineFrom(in, out, "New plan (starter/pro): ")
	if err != nil {
		return c, err
	}
	c.DownloadDir, c.Plan = strings.TrimSpace(dir), config.Plan(strings.TrimSpace(plan))
	if err := c.Validate(); err != nil {
		return c, err
	}
	if err := os.MkdirAll(c.DownloadDir, 0700); err != nil {
		return c, err
	}
	p, err := config.DefaultPath()
	if err != nil {
		return c, err
	}
	if err := config.Save(p, c); err != nil {
		return c, err
	}
	fmt.Fprintln(out, "configuration updated")
	return c, nil
}

func chooseGame(ctx context.Context, in io.Reader, out io.Writer, s Service, r api.SearchResponse) error {
	for i, g := range r.Games {
		fmt.Fprintf(out, "%d. %s (%s)\n", i+1, g.Name, g.AppID)
	}
	i, err := readSelection(in, len(r.Games))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\nImage: %s\n", r.Games[i].Name, r.Games[i].HeaderImageURL)
	renderImage(out, r.Games[i].HeaderImageURL)
	fmt.Fprint(out, "Download? (y/n): ")
	if !yes(in) {
		return nil
	}
	var p string
	if named, ok := s.(namedDownloader); ok {
		p, err = named.DownloadGameNamed(ctx, r.Games[i].AppID, r.Games[i].Name)
	} else {
		p, err = s.DownloadGame(ctx, r.Games[i].AppID)
	}
	if err == nil {
		fmt.Fprintln(out, p)
	}
	return err
}

func downloadIDs(ctx context.Context, ids []string, out, errOut io.Writer, s Service) error {
	if len(ids) == 0 {
		return errors.New("at least one AppID is required")
	}
	var firstErr error
	for _, id := range ids {
		p, err := s.DownloadGame(ctx, id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			fmt.Fprintf(errOut, "%s: %v\n", id, err)
			continue
		}
		fmt.Fprintln(out, p)
	}
	return firstErr
}

func splitIDs(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	ids := make([]string, 0, len(fields))
	for _, id := range fields {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func showHistory(out io.Writer, s Service, query string) error {
	lister, ok := s.(historyLister)
	if !ok {
		fmt.Fprintln(out, "history is unavailable")
		return nil
	}
	entries, err := lister.HistoryEntries()
	if err != nil {
		return err
	}
	count := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if query != "" && !strings.Contains(strings.ToLower(e.Query+" "+e.Name+" "+e.AppID), strings.ToLower(query)) {
			continue
		}
		fmt.Fprintf(out, "%s  %-13s %-12s %s%s\n", e.At.Local().Format("2006-01-02 15:04"), e.Kind, e.AppID, e.Name, historyPath(e.Path))
		count++
	}
	if count == 0 {
		fmt.Fprintln(out, "no history entries")
	}
	return nil
}

func historyPath(path string) string {
	if path == "" {
		return ""
	}
	return " -> " + path
}

func renderImage(out io.Writer, imageURL string) {
	if imageURL == "" {
		return
	}
	if _, err := exec.LookPath("chafa"); err != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	tmp, err := os.CreateTemp("", "dbox-image-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return
	}
	if err = tmp.Close(); err != nil {
		return
	}
	cmd := exec.Command("chafa", "--format=symbols", filepath.Clean(name))
	if preview, err := cmd.Output(); err == nil {
		fmt.Fprintln(out, string(preview))
	}
}
func chooseFix(ctx context.Context, in io.Reader, out io.Writer, s Service, r api.FixesResponse) error {
	for i, f := range r.Fixes {
		fmt.Fprintf(out, "%d. %s (%s)\n", i+1, f.Name, f.File)
	}
	i, err := readSelection(in, len(r.Fixes))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\nDownload? (y/n): ", r.Fixes[i].Name)
	if !yes(in) {
		return nil
	}
	p, err := s.DownloadFix(ctx, r.Fixes[i])
	if err == nil {
		fmt.Fprintln(out, p)
	}
	return err
}
func readSelection(in io.Reader, n int) (int, error) {
	line, err := readInputLine(in)
	if err != nil && line == "" {
		return 0, errors.New("selection required")
	}
	return parseSelection([]string{line}, n)
}

func readInputLine(in io.Reader) (string, error) {
	if r, ok := in.(*bufio.Reader); ok {
		line, err := r.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err
	}
	var b []byte
	one := []byte{0}
	for {
		n, err := in.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return strings.TrimRight(string(b), "\r"), nil
			}
			b = append(b, one[0])
		}
		if err != nil {
			return string(b), err
		}
	}
}
func parseSelection(lines []string, n int) (int, error) {
	if n == 0 {
		return 0, errors.New("no results")
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return 0, errors.New("selection required")
	}
	i, e := strconv.Atoi(strings.TrimSpace(lines[0]))
	if e != nil || i < 1 || i > n {
		return 0, errors.New("invalid selection")
	}
	return i - 1, nil
}
func yes(in io.Reader) bool {
	line, _ := readInputLine(in)
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}
