package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

func Run(args []string, in io.Reader, out, errOut io.Writer) error {
	p, err := config.DefaultPath()
	if err != nil {
		return err
	}
	c, err := config.Load(p)
	if err != nil {
		all := strings.Split(readAll(in), "\n")
		if len(all) < 3 {
			return errors.New("configuration is not set up")
		}
		c, err = Setup(strings.NewReader(strings.Join(all[:3], "\n")), out, p)
		if err != nil {
			return err
		}
		in = strings.NewReader(strings.Join(all[3:], "\n"))
	}
	return RunWithService(args, in, out, errOut, app.New(c, nil), c)
}

func Setup(in io.Reader, out io.Writer, path string) (config.Config, error) {
	lines := strings.Split(strings.TrimSpace(readAll(in)), "\n")
	if len(lines) < 3 {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	c := config.Config{APIKey: strings.TrimSpace(lines[0]), DownloadDir: strings.TrimSpace(lines[1]), Plan: config.Plan(strings.TrimSpace(lines[2]))}
	fmt.Fprintln(out, "Depotbox setup")
	fmt.Fprintln(out, "API key configured")
	if err := c.Validate(); err != nil {
		return config.Config{}, err
	}
	if err := os.MkdirAll(c.DownloadDir, 0700); err != nil {
		return config.Config{}, err
	}
	return c, config.Save(path, c)
}
func readAll(in io.Reader) string { b, _ := io.ReadAll(in); return string(b) }
func RunWithService(args []string, in io.Reader, out, errOut io.Writer, s Service, c config.Config) error {
	ctx := context.Background()
	if len(args) == 0 {
		fmt.Fprintln(out, "1. Search games\n2. Download by AppID\n3. Game fixes\n4. Config\n5. Exit")
		return nil
	}
	switch args[0] {
	case "search":
		if len(args) != 2 {
			return errors.New("usage: dbox search <term>")
		}
		r, e := s.Search(ctx, args[1])
		if e != nil {
			return e
		}
		return chooseGame(ctx, in, out, s, r)
	case "download":
		if len(args) != 2 {
			return errors.New("usage: dbox download <appid>")
		}
		p, e := s.DownloadGame(ctx, args[1])
		if e == nil {
			fmt.Fprintln(out, p)
		}
		return e
	case "fixes":
		q := ""
		if len(args) > 2 {
			return errors.New("usage: dbox fixes [query]")
		}
		if len(args) == 2 {
			q = args[1]
		}
		r, e := s.Fixes(ctx, q, "")
		if e != nil {
			return e
		}
		return chooseFix(ctx, in, out, s, r)
	case "config":
		fmt.Fprintf(out, "Download directory: %s\nPlan: %s\nAPI key: configured\n", c.DownloadDir, c.Plan)
		if v := strings.Split(strings.TrimSpace(readAll(in)), "\n"); len(v) >= 2 && v[0] != "" {
			c.DownloadDir, c.Plan = v[0], config.Plan(v[1])
			if err := c.Validate(); err != nil {
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
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func chooseGame(ctx context.Context, in io.Reader, out io.Writer, s Service, r api.SearchResponse) error {
	for i, g := range r.Games {
		fmt.Fprintf(out, "%d. %s (%s)\n", i+1, g.Name, g.AppID)
	}
	input, _ := io.ReadAll(in)
	lines := strings.Split(strings.TrimSpace(string(input)), "\n")
	i, e := parseSelection(lines, len(r.Games))
	if e != nil {
		return e
	}
	fmt.Fprintf(out, "%s\nDownload? (y/n): ", r.Games[i].Name)
	if len(lines) < 2 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[1])), "y") {
		return nil
	}
	p, e := s.DownloadGame(ctx, r.Games[i].AppID)
	if e == nil {
		fmt.Fprintln(out, p)
	}
	return e
}
func chooseFix(ctx context.Context, in io.Reader, out io.Writer, s Service, r api.FixesResponse) error {
	for i, f := range r.Fixes {
		fmt.Fprintf(out, "%d. %s (%s)\n", i+1, f.Name, f.File)
	}
	input, _ := io.ReadAll(in)
	lines := strings.Split(strings.TrimSpace(string(input)), "\n")
	i, e := parseSelection(lines, len(r.Fixes))
	if e != nil {
		return e
	}
	fmt.Fprintf(out, "%s\nDownload? (y/n): ", r.Fixes[i].Name)
	if e == nil && len(lines) >= 2 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[1])), "y") {
		p, e := s.DownloadFix(ctx, r.Fixes[i])
		if e == nil {
			fmt.Fprintln(out, p)
		}
		return e
	}
	return nil
}
func readSelection(in io.Reader, n int) (int, error) {
	b, _ := io.ReadAll(in)
	return parseSelection(strings.Split(strings.TrimSpace(string(b)), "\n"), n)
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
