package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rufi/dbox/internal/api"
	"github.com/rufi/dbox/internal/app"
	"github.com/rufi/dbox/internal/config"
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
		if len(args) != 2 {
			return errors.New("usage: dbox download <appid>")
		}
		p, err := s.DownloadGame(ctx, args[1])
		if err == nil {
			fmt.Fprintln(out, p)
		}
		return err
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
		return configCommand(in, out, c, len(args) == 2 && args[1] == "edit")
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func menu(ctx context.Context, in io.Reader, out io.Writer, s Service, c config.Config) error {
	r := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "1. Search games\n2. Download by AppID\n3. Game fixes\n4. Config\n5. Exit")
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
			id, e := readLine(r, out, "AppID: ")
			if e != nil {
				return e
			}
			p, e := s.DownloadGame(ctx, strings.TrimSpace(id))
			if e != nil {
				return e
			}
			fmt.Fprintln(out, p)
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
			if e := configCommand(r, out, c, true); e != nil {
				return e
			}
		case "5":
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

func configCommand(in io.Reader, out io.Writer, c config.Config, edit bool) error {
	fmt.Fprintf(out, "Download directory: %s\nPlan: %s\nAPI key: configured\n", c.DownloadDir, c.Plan)
	if !edit {
		return nil
	}
	r := bufio.NewReader(in)
	dir, err := readLine(r, out, "New download directory: ")
	if err != nil {
		return nil
	}
	plan, err := readLine(r, out, "New plan (starter/pro): ")
	if err != nil {
		return err
	}
	c.DownloadDir, c.Plan = strings.TrimSpace(dir), config.Plan(strings.TrimSpace(plan))
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(c.DownloadDir, 0700); err != nil {
		return err
	}
	p, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if err := config.Save(p, c); err != nil {
		return err
	}
	fmt.Fprintln(out, "configuration updated")
	return nil
}

func chooseGame(ctx context.Context, in io.Reader, out io.Writer, s Service, r api.SearchResponse) error {
	for i, g := range r.Games {
		fmt.Fprintf(out, "%d. %s (%s)\n", i+1, g.Name, g.AppID)
	}
	i, err := readSelection(in, len(r.Games))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\nDownload? (y/n): ", r.Games[i].Name)
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
	b, _ := io.ReadAll(in)
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(b))), "y")
}
