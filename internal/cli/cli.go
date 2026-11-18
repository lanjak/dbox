package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"git.lanjak.net/rufi/dbox/internal/api"
	"git.lanjak.net/rufi/dbox/internal/app"
	"git.lanjak.net/rufi/dbox/internal/config"
	"git.lanjak.net/rufi/dbox/internal/download"
	"git.lanjak.net/rufi/dbox/internal/history"
	"git.lanjak.net/rufi/dbox/internal/steam"
)

// Version is set by cmd/dbox from the build flags.
var Version = "dev"

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
type handoffService interface {
	Handoff(string) error
}
type handoffEnabledService interface {
	HandoffEnabled() bool
}
type installedLister interface {
	InstalledApps() ([]steam.App, error)
}

// extendedService is the rest of the DepotBox API. *app.App implements it.
type extendedService interface {
	DownloadBatch(context.Context, []string, bool) (string, error)
	DownloadLua(context.Context, string) (string, error)
	GameDetails(context.Context, string) (api.GameDetails, error)
	Availability(context.Context, []string) ([]api.Availability, error)
	Usage(context.Context) (api.Usage, error)
	Stats(context.Context) (api.ServiceStats, error)
	RequestGame(context.Context, string) (api.GameRequest, error)
	GameRequests(context.Context) ([]api.GameRequest, error)
	Lists(context.Context, int) (api.ListsPage, error)
	List(context.Context, string) (api.List, error)
	UpdateGameList(context.Context) (int, error)
	Game(context.Context, string) (api.Game, error)
}

// hideKeyInput is true when stdin is a terminal, so API key prompts can turn off echo.
var hideKeyInput bool

// downloads counts running downloads. Ctrl-C cancels a running download
// cleanly; with none running, it restores the terminal and exits at once.
var downloads atomic.Int32

// handleInterrupt makes Ctrl-C cancel ctx while a download runs, and exit
// with status 130 otherwise. A second Ctrl-C always exits.
func handleInterrupt(cancel context.CancelFunc, errOut io.Writer) func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		canceled := false
		for range sig {
			if downloads.Load() > 0 && !canceled {
				canceled = true
				cancel()
				continue
			}
			restoreTerminal()
			fmt.Fprintln(errOut, "\ncanceled")
			os.Exit(130)
		}
	}()
	return func() { signal.Stop(sig) }
}

const usageText = `dbox - DepotBox from the terminal

Usage:
  dbox                              interactive menu
  dbox search [filters] <term>      search, pick a game, download it
        --dlc | --no-dlc            only DLC | no DLC
        --drm | --no-drm            only games with | without a DRM notice
        --available                 only games with a manifest
        --limit N                   up to N results (default 25, max 100)
  dbox download [--zip [--merge]] <appid>...
                                    download games; --zip packs them into one
                                    batch ZIP (up to 50 per ZIP)
  dbox info <appid>                 game details, availability and DLCs
  dbox check <appid>...             check availability without downloading
  dbox lua <appid>...               download only the .lua script
  dbox fixes [--tag T] [query]      find and download game fixes
                                    (T: online, bypass, hypervisor)
  dbox lists [page]                 browse community lists
  dbox list <list-id>               show a list and download its games
  dbox request <appid>              ask DepotBox to add a missing game
  dbox requests                     show your game requests
  dbox usage                        your API usage and service stats
  dbox refresh                      download the DepotBox game list now
                                    (used for close matches; free, and
                                    also refreshed when dbox starts)
  dbox install <archive.zip>        hand a ZIP to the configured handoff target
  dbox installed                    list the games in your Steam libraries,
                                    and which ones dbox set up
  dbox history [term]               recent searches and downloads
  dbox config [edit]                show or change settings
  dbox version                      print the version
`

func Run(args []string, in io.Reader, out, errOut io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			fmt.Fprint(out, usageText)
			return nil
		case "version", "-v", "--version":
			fmt.Fprintln(out, "dbox", Version)
			return nil
		}
	}
	if f, ok := in.(*os.File); ok && isTerminal(f) {
		hideKeyInput = true
		if runtime.GOOS != "windows" && saveTerminal() {
			lineEdit = true
			in = bufio.NewReader(f) // one shared reader, so no typed input is lost
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer handleInterrupt(cancel, errOut)()
	p, err := config.DefaultPath()
	if err != nil {
		return err
	}
	c, err := config.Load(p)
	if errors.Is(err, fs.ErrNotExist) {
		if c, err = Setup(in, out, p); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("load config %s: %w (fix the file, or delete it to run setup again)", p, err)
	}
	// Only the interactive menu checks for updates; scripts and one-off commands never wait on it.
	if len(args) == 0 && lineEdit {
		if done, err := offerUpdate(ctx, in, out); done || err != nil {
			return err
		}
	}
	a := app.New(c, nil)
	if len(args) == 0 || args[0] == "search" {
		a.RefreshCatalog(ctx) // background; the close-match search uses it
	}
	err = run(ctx, args, in, out, errOut, a, c)
	if ctx.Err() != nil {
		return context.Canceled
	}
	return err
}

func Setup(in io.Reader, out io.Writer, path string) (config.Config, error) {
	u := newUI(out, out)
	u.title("Welcome to dbox - first-time setup")
	fmt.Fprintln(out, "Get an API key from your DepotBox dashboard: https://depotbox.lol")
	fmt.Fprintln(out)
	key, err := readAPIKey(in, out)
	if err != nil {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	defDir := defaultDownloadDir()
	dir, err := readLineFrom(in, out, fmt.Sprintf("Download directory [%s]: ", defDir))
	if err != nil && dir == "" {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	plan, err := readLineFrom(in, out, "Plan (starter/pro) [starter]: ")
	if err != nil && plan == "" && !errors.Is(err, io.EOF) {
		return config.Config{}, errors.New("setup requires API key, download directory, and plan")
	}
	c := config.Config{
		APIKey:        strings.TrimSpace(key),
		DownloadDir:   expandHome(orDefault(dir, defDir)),
		Plan:          config.Plan(strings.ToLower(orDefault(plan, string(config.Starter)))),
		HandoffTarget: config.HandoffNone,
	}
	if err := c.Validate(); err != nil {
		return config.Config{}, err
	}
	if err := os.MkdirAll(c.DownloadDir, 0700); err != nil {
		return config.Config{}, err
	}
	if err := config.Save(path, c); err != nil {
		return config.Config{}, err
	}
	u.ok("API key configured. Settings saved to %s", path)
	return c, nil
}

func orDefault(value, def string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return def
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "dbox-downloads"
	}
	return filepath.Join(home, "Downloads", "dbox")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func readAPIKey(input io.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "API key: ")
	if hideKeyInput {
		if setEcho(false); termDirty.Load() {
			defer func() { setEcho(true); fmt.Fprintln(out) }()
		}
	}
	return plainLine(input) // no line editor: it would draw the hidden key
}
func isTerminal(f *os.File) bool {
	i, err := f.Stat()
	return err == nil && i.Mode()&os.ModeCharDevice != 0
}
func readLine(r *bufio.Reader, out io.Writer, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(out, prompt)
	}
	return readInputLine(r)
}

func readLineFrom(in io.Reader, out io.Writer, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(out, prompt)
	}
	return readInputLine(in)
}

func RunWithService(args []string, in io.Reader, out, errOut io.Writer, s Service, c config.Config) error {
	return run(context.Background(), args, in, out, errOut, s, c)
}

type session struct {
	ctx         context.Context
	in          io.Reader
	out, errOut io.Writer
	s           Service
	c           config.Config
	ui          *ui
}

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, s Service, c config.Config) error {
	ss := &session{ctx: ctx, in: in, out: out, errOut: errOut, s: s, c: c, ui: newUI(out, errOut)}
	if a, ok := s.(*app.App); ok {
		a.Progress = ss.ui.progress
	}
	if len(args) == 0 {
		return ss.menu()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "info", "check", "lua", "request":
		rest = appIDsFrom(rest)
	}
	switch cmd {
	case "search":
		flags, pos, err := parseArgs(rest, map[string]bool{"dlc": false, "no-dlc": false, "drm": false, "no-drm": false, "available": false, "limit": true})
		if err != nil {
			return err
		}
		if len(pos) == 0 {
			return errors.New("usage: dbox search [filters] <term>")
		}
		req := api.SearchRequest{SearchTerm: strings.Join(pos, " ")}
		if err := applySearchFlags(&req, flags); err != nil {
			return err
		}
		return ss.search(req)
	case "download":
		flags, ids, err := parseArgs(rest, map[string]bool{"zip": false, "merge": false})
		if err != nil {
			return err
		}
		ids = appIDsFrom(ids)
		if err := checkIDs(ids); err != nil {
			return errors.New("usage: dbox download [--zip [--merge]] <appid>...: " + err.Error())
		}
		if _, ok := flags["zip"]; ok {
			_, merge := flags["merge"]
			return ss.downloadBatch(ids, merge)
		}
		return ss.downloadIDs(ids, false)
	case "info":
		if len(rest) != 1 || checkIDs(rest) != nil {
			return errors.New("usage: dbox info <appid>")
		}
		return ss.info(rest[0], false)
	case "check":
		if err := checkIDs(rest); err != nil {
			return errors.New("usage: dbox check <appid>...: " + err.Error())
		}
		return ss.check(rest)
	case "lua":
		if err := checkIDs(rest); err != nil {
			return errors.New("usage: dbox lua <appid>...: " + err.Error())
		}
		return ss.lua(rest)
	case "fixes":
		flags, pos, err := parseArgs(rest, map[string]bool{"tag": true})
		if err != nil {
			return err
		}
		return ss.fixes(strings.Join(pos, " "), flags["tag"])
	case "lists":
		page := 1
		if len(rest) > 1 {
			return errors.New("usage: dbox lists [page]")
		}
		if len(rest) == 1 {
			n, err := strconv.Atoi(rest[0])
			if err != nil || n < 1 {
				return errors.New("usage: dbox lists [page]")
			}
			page = n
		}
		return ss.browseLists(page)
	case "list":
		if len(rest) != 1 {
			return errors.New("usage: dbox list <list-id>")
		}
		return ss.showList(rest[0])
	case "request":
		if len(rest) != 1 || checkIDs(rest) != nil {
			return errors.New("usage: dbox request <appid>")
		}
		return ss.request(rest[0])
	case "requests":
		return ss.requests()
	case "usage", "stats":
		return ss.usage()
	case "refresh":
		return ss.refreshGameList()
	case "install":
		if len(rest) != 1 {
			return errors.New("usage: dbox install <archive.zip>")
		}
		enabled, ok := s.(handoffEnabledService)
		if !ok || !enabled.HandoffEnabled() {
			return errors.New("enable a handoff target first (dbox config edit)")
		}
		h, ok := s.(handoffService)
		if !ok {
			return errors.New("the configured service cannot install a ZIP")
		}
		if err := h.Handoff(rest[0]); err != nil {
			return err
		}
		ss.ui.ok("Installed: %s", rest[0])
		return nil
	case "installed":
		return ss.installed()
	case "history":
		return ss.showHistory(strings.Join(rest, " "), 0)
	case "config":
		if len(rest) > 1 || (len(rest) == 1 && rest[0] != "edit") {
			return errors.New("usage: dbox config [edit]")
		}
		_, err := ss.configCommand(in, len(rest) == 1)
		return err
	case "help", "-h", "--help":
		fmt.Fprint(out, usageText)
		return nil
	case "version", "-v", "--version":
		fmt.Fprintln(out, "dbox", Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (run dbox help)", cmd)
	}
}

// parseArgs splits --flags from positional arguments. allowed maps a flag
// name to whether it takes a value ("--limit 5" or "--limit=5").
func parseArgs(args []string, allowed map[string]bool) (map[string]string, []string, error) {
	flags := map[string]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		takesValue, ok := allowed[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown flag --%s (run dbox help)", name)
		}
		if takesValue && !hasValue {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = args[i]
		}
		flags[name] = value
	}
	return flags, pos, nil
}

func applySearchFlags(req *api.SearchRequest, flags map[string]string) error {
	for name, value := range flags {
		switch name {
		case "dlc":
			req.FilterDLC = "only"
		case "no-dlc":
			req.FilterDLC = "exclude"
		case "drm":
			req.FilterProtection = "require"
		case "no-drm":
			req.FilterProtection = "exclude"
		case "available":
			req.FilterAvailability = true
		case "limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100 {
				return errors.New("--limit takes a number from 1 to 100")
			}
			req.Limit = n
		}
	}
	return nil
}

// applySearchFilters reads the menu's comma-separated filter words.
func applySearchFilters(req *api.SearchRequest, text string) {
	for _, p := range strings.Split(strings.ToLower(strings.TrimSpace(text)), ",") {
		p = strings.TrimSpace(p)
		switch {
		case p == "dlc":
			req.FilterDLC = "only"
		case p == "no-dlc" || p == "nodlc":
			req.FilterDLC = "exclude"
		case p == "drm":
			req.FilterProtection = "require"
		case p == "no-drm" || p == "nodrm":
			req.FilterProtection = "exclude"
		case p == "available":
			req.FilterAvailability = true
		case strings.HasPrefix(p, "protection="):
			if v := strings.TrimSpace(strings.TrimPrefix(p, "protection=")); v == "require" || v == "exclude" {
				req.FilterProtection = v
			}
		}
	}
}

func validAppID(id string) bool {
	if id == "" || len(id) > 10 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// appIDFrom accepts a bare AppID or a Steam store or SteamDB link that contains /app/<id>.
func appIDFrom(s string) string {
	s = strings.TrimSpace(s)
	if _, rest, ok := strings.Cut(s, "/app/"); ok {
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		return rest[:end]
	}
	return s
}

func appIDsFrom(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = appIDFrom(id)
	}
	return out
}

func checkIDs(ids []string) error {
	if len(ids) == 0 {
		return errors.New("at least one AppID is required")
	}
	for _, id := range ids {
		if !validAppID(id) {
			return fmt.Errorf("%q is not a Steam AppID", id)
		}
	}
	return nil
}

func (ss *session) ext() (extendedService, error) {
	x, ok := ss.s.(extendedService)
	if !ok {
		return nil, errors.New("this command needs the DepotBox client")
	}
	return x, nil
}

// --- interactive menu -------------------------------------------------------

var menuItems = []struct{ key, label string }{
	{"1", "Search games"},
	{"2", "Download by AppID"},
	{"3", "Game info"},
	{"4", "Lua script only"},
	{"5", "Game fixes"},
	{"6", "Community lists"},
	{"7", "Request a missing game"},
	{"8", "Usage and service stats"},
	{"9", "History"},
	{"i", "Installed games"},
	{"r", "Refresh game list"},
	{"c", "Config"},
	{"q", "Quit"},
}

func (ss *session) menu() error {
	r := bufio.NewReader(ss.in)
	ss.in = r
	for {
		if ss.ctx.Err() != nil {
			return nil
		}
		clearScreen(ss.out)
		fmt.Fprintf(ss.out, "%s %s\n\n", ss.ui.bold(ss.ui.cyan("dbox")), ss.ui.dim("DepotBox CLI "+Version))
		for _, it := range menuItems {
			fmt.Fprintf(ss.out, "  %s. %s\n", ss.ui.cyan(it.key), it.label)
		}
		fmt.Fprintln(ss.out)
		choice, err := readLine(r, ss.out, ss.ui.bold("Select an option: "))
		if err != nil && choice == "" {
			return nil
		}
		ss.ui.shown = false
		var e error
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "1":
			e = ss.menuSearch()
		case "2":
			e = ss.menuDownload()
		case "3":
			if id, ok, err := ss.askID("Game info", "AppID or Steam link: "); err != nil || !ok {
				e = err
			} else {
				e = ss.info(id, true)
			}
		case "4":
			e = ss.menuIDs("Lua script only", "AppIDs or Steam links: ", ss.lua)
		case "5":
			e = ss.menuFixes()
		case "6":
			e = ss.browseLists(1)
		case "7":
			if id, ok, err := ss.askID("Request a missing game", "AppID or Steam link: "); err != nil || !ok {
				e = err
			} else if e = ss.request(id); e == nil {
				fmt.Fprintln(ss.out)
				e = ss.requests()
			}
		case "8":
			e = ss.usage()
		case "9":
			clearScreen(ss.out)
			e = ss.showHistory("", 20)
		case "i":
			clearScreen(ss.out)
			e = ss.installed()
		case "r":
			ss.screen("Refresh game list")
			e = ss.refreshGameList()
		case "c":
			clearScreen(ss.out)
			updated, err := ss.configCommand(r, true)
			if err == nil {
				ss.c = updated
				if updater, ok := ss.s.(configUpdater); ok {
					updater.UpdateConfig(updated)
				}
				ss.ui.shown = false // the editor showed its own result
			}
			e = err
		case "q", "0", "quit", "exit":
			return nil
		case "":
		default:
			e = fmt.Errorf("invalid selection %q", strings.TrimSpace(choice))
		}
		if errors.Is(e, io.EOF) || ss.ctx.Err() != nil {
			return nil
		}
		if e != nil {
			fmt.Fprintln(ss.out)
			ss.ui.fail(ss.out, "%s", capitalize(e.Error()))
		}
		// The next loop clears the screen, so let the user read the result first.
		if ss.ui.shown {
			fmt.Fprintln(ss.out)
			if _, err := ss.prompt("Press Enter to return to menu: "); err != nil {
				return nil
			}
		}
	}
}

func (ss *session) prompt(text string) (string, error) {
	line, err := readLineFrom(ss.in, ss.out, ss.ui.bold(text))
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// screen starts a new screen: it clears the terminal and prints a title.
// A title alone is not a result, so the menu does not pause for it.
func (ss *session) screen(title string) {
	clearScreen(ss.out)
	ss.ui.title(title)
	fmt.Fprintln(ss.out)
	ss.ui.shown = false
}

// ask prints a y/n question with a blank line before it, and a blank line
// after a "yes" so the output that follows stands apart.
func (ss *session) ask(question string) bool {
	fmt.Fprintf(ss.out, "\n%s", ss.ui.bold(question+" (y/n): "))
	if !ss.yes() {
		return false
	}
	fmt.Fprintln(ss.out)
	return true
}

// askID reads one AppID or Steam link. ok is false when the user leaves it blank.
func (ss *session) askID(title, text string) (string, bool, error) {
	ss.screen(title)
	id, err := ss.prompt(text)
	if err != nil || id == "" {
		return "", false, err
	}
	if id = appIDFrom(id); !validAppID(id) {
		return "", false, fmt.Errorf("%q is not a Steam AppID", id)
	}
	fmt.Fprintln(ss.out)
	return id, true, nil
}

func (ss *session) menuIDs(title, text string, fn func([]string) error) error {
	ss.screen(title)
	line, err := ss.prompt(text)
	if err != nil || line == "" {
		return err
	}
	ids := splitIDs(line)
	if err := checkIDs(ids); err != nil {
		return err
	}
	fmt.Fprintln(ss.out)
	return fn(ids)
}

func (ss *session) menuSearch() error {
	ss.screen("Search games")
	term, err := ss.prompt("Search term: ")
	if err != nil || term == "" {
		return err
	}
	filters, err := ss.prompt("Filters (dlc, no-dlc, drm, no-drm, available; Enter for none): ")
	if err != nil {
		return err
	}
	req := api.SearchRequest{SearchTerm: term}
	applySearchFilters(&req, filters)
	return ss.search(req)
}

func (ss *session) menuDownload() error {
	return ss.menuIDs("Download by AppID", "AppIDs or Steam links, separated by spaces or commas: ", func(ids []string) error {
		if len(ids) > 1 {
			if _, ok := ss.s.(extendedService); ok {
				if ss.ask("Pack them into one batch ZIP?") {
					return ss.downloadBatch(ids, false)
				}
				fmt.Fprintln(ss.out)
			}
		}
		return ss.downloadIDs(ids, true)
	})
}

func (ss *session) menuFixes() error {
	ss.screen("Game fixes")
	q, err := ss.prompt("Fix search (game name or AppID, Enter for all): ")
	if err != nil {
		return err
	}
	tags, err := ss.prompt("Tags (online, bypass, hypervisor; Enter for all): ")
	if err != nil {
		return err
	}
	return ss.fixes(q, tags)
}

// --- search and download ----------------------------------------------------

func (ss *session) search(req api.SearchRequest) error {
	var result api.SearchResponse
	var err error
	if fsr, ok := ss.s.(filteredSearcher); ok {
		result, err = fsr.SearchWithFilters(ss.ctx, req)
	} else {
		result, err = ss.s.Search(ss.ctx, req.SearchTerm)
	}
	if err != nil {
		return err
	}
	clearScreen(ss.out)
	if result.Approximate && (req.FilterDLC != "" || req.FilterProtection != "" || req.FilterAvailability) {
		ss.ui.warn("Filters do not apply to close matches.")
		fmt.Fprintln(ss.out)
	}
	return ss.chooseGame(result, req.SearchTerm)
}

func (ss *session) gameTags(g api.Game) string {
	var tags []string
	if g.IsDLC {
		tags = append(tags, ss.ui.yellow("DLC"))
	}
	if g.DRMNotice != nil && *g.DRMNotice != "" {
		tags = append(tags, ss.ui.red("DRM"))
	}
	if g.ExternalUserAccountNotice != nil && *g.ExternalUserAccountNotice != "" {
		tags = append(tags, ss.ui.yellow("ACCOUNT"))
	}
	return strings.Join(tags, " ")
}

func (ss *session) chooseGame(r api.SearchResponse, term string) error {
	if len(r.Games) == 0 {
		ss.ui.warn("No games found for %q.", term)
		return nil
	}
	heading := "Results for"
	if r.Approximate {
		heading = "Close matches for"
	}
	ss.ui.title(fmt.Sprintf("%s %q (%d)", heading, term, len(r.Games)))
	if r.Approximate {
		fmt.Fprintln(ss.out, ss.ui.dim("No game name contains this exact text, so these are the closest names."))
		fmt.Fprintln(ss.out)
	}
	ss.gameRows(r.Games)
	i, ok, err := ss.readSelection(len(r.Games), "game")
	if err != nil || !ok {
		return err
	}
	return ss.openGame(r.Games[i], r.Approximate)
}

func (ss *session) gameRows(games []api.Game) {
	for i, g := range games {
		fmt.Fprintf(ss.out, "%3d. %s %s %s\n", i+1, pad(g.Name, 46), ss.ui.dim(pad(string(g.AppID), 9)), ss.gameTags(g))
	}
}

// openGame shows one game with its image and offers to download it. When the
// game came from a list with only AppIDs and names, it loads the full data first.
func (ss *session) openGame(g api.Game, partial bool) error {
	if x, ok := ss.s.(extendedService); ok && partial {
		if full, err := x.Game(ss.ctx, string(g.AppID)); err == nil {
			g = full
		}
	}
	clearScreen(ss.out)
	ss.renderImage(headerImage(g))
	ss.printGameDetails(g)
	if !ss.ask("Download?") {
		return nil
	}
	return ss.downloadOne(string(g.AppID), g.Name, true)
}

// downloadOne downloads one game, then hands it off. interactive enables follow-up questions.
func (ss *session) downloadOne(id, name string, interactive bool) error {
	label := id
	if name != "" {
		label = name
	}
	act := ss.ui.start("Downloading " + label)
	var p string
	var err error
	if named, ok := ss.s.(namedDownloader); ok && name != "" {
		p, err = named.DownloadGameNamed(ss.ctx, id, name)
	} else {
		p, err = ss.s.DownloadGame(ss.ctx, id)
	}
	summary := act.end()
	switch {
	case errors.Is(err, download.ErrFileExists) && p != "":
		ss.ui.warn("Already downloaded: %s", p)
		if ss.handoffEnabled() {
			if err := ss.handoff(p); err != nil {
				return err
			}
			ss.ui.ok("Handed off existing download: %s", p)
		}
		return nil
	case api.IsNotFound(err):
		ss.ui.fail(ss.out, "DepotBox has no depot keys for %s (AppID %s).", label, id)
		if interactive {
			return ss.offerRequest(id)
		}
		fmt.Fprintf(ss.out, "  Ask for it with: dbox request %s\n", id)
		return err
	case err != nil:
		return err
	}
	ss.ui.ok("Saved %s %s", p, ss.ui.dim("("+summary+")"))
	if ss.handoffEnabled() {
		if err := ss.handoff(p); err != nil {
			return err
		}
		ss.ui.ok("Handed off to %s", ss.c.HandoffTarget)
	}
	return nil
}

func (ss *session) offerRequest(id string) error {
	if _, ok := ss.s.(extendedService); !ok {
		return nil
	}
	if !ss.ask("Ask DepotBox staff to add it?") {
		return nil
	}
	return ss.request(id)
}

func (ss *session) handoffEnabled() bool {
	e, ok := ss.s.(handoffEnabledService)
	_, can := ss.s.(handoffService)
	return ok && can && e.HandoffEnabled()
}

func (ss *session) handoff(path string) error {
	return ss.s.(handoffService).Handoff(path)
}

func (ss *session) printGameDetails(g api.Game) {
	ss.ui.title(g.Name)
	ss.ui.field("AppID", string(g.AppID))
	ss.ui.field("DLC", yesNo(g.IsDLC))
	ss.ui.field("DRM notice", optionalText(g.DRMNotice))
	ss.ui.field("Account notice", optionalText(g.ExternalUserAccountNotice))
	ss.ui.field("Store page", "https://store.steampowered.com/app/"+string(g.AppID))
	if img := headerImage(g); img != "" {
		ss.ui.field("Header image", img)
	}
}

// headerImage is the game's header image, or the standard Steam address for it.
func headerImage(g api.Game) string {
	if g.HeaderImageURL != "" || g.AppID == "" {
		return g.HeaderImageURL
	}
	return "https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/" + string(g.AppID) + "/header.jpg"
}

func yesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func optionalText(value *string) string {
	if value == nil || *value == "" {
		return "None"
	}
	return *value
}

// downloadIDs downloads games one after another. An existing file is a skip,
// not a failure; the first real error is returned after all IDs are tried.
func (ss *session) downloadIDs(ids []string, interactive bool) error {
	var firstErr error
	failed := 0
	for _, id := range ids {
		if ss.ctx.Err() != nil {
			return ss.ctx.Err()
		}
		if err := ss.downloadOne(id, "", interactive && len(ids) == 1); err != nil {
			if ss.ctx.Err() != nil {
				return ss.ctx.Err()
			}
			if len(ids) > 1 && !api.IsNotFound(err) {
				ss.ui.fail(ss.errOut, "%s: %v", id, err)
			}
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if len(ids) > 1 {
		fmt.Fprintf(ss.out, "%s\n", ss.ui.dim(fmt.Sprintf("%d of %d done, %d failed", len(ids)-failed, len(ids), failed)))
	}
	if firstErr != nil && len(ids) > 1 {
		return fmt.Errorf("%d of %d downloads failed", failed, len(ids))
	}
	return firstErr
}

// downloadBatch saves the games as batch ZIPs of up to api.MaxBatch AppIDs each.
func (ss *session) downloadBatch(ids []string, merge bool) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	for start := 0; start < len(ids); start += api.MaxBatch {
		chunk := ids[start:min(start+api.MaxBatch, len(ids))]
		act := ss.ui.start(fmt.Sprintf("Building batch ZIP of %d game(s)", len(chunk)))
		p, err := x.DownloadBatch(ss.ctx, chunk, merge)
		summary := act.end()
		if err != nil {
			return err
		}
		ss.ui.ok("Saved %s %s", p, ss.ui.dim("("+summary+")"))
		fmt.Fprintln(ss.out, ss.ui.dim("  Games that could not be built are listed in excluded_manifests.txt inside the ZIP."))
	}
	return nil
}

func (ss *session) lua(ids []string) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range ids {
		p, err := x.DownloadLua(ss.ctx, id)
		switch {
		case errors.Is(err, download.ErrFileExists):
			ss.ui.warn("Already downloaded: %s", p)
		case api.IsNotFound(err):
			ss.ui.fail(ss.out, "No depot keys for AppID %s, so there is no .lua script.", id)
		case err != nil:
			ss.ui.fail(ss.out, "%s: %v", id, err)
		default:
			ss.ui.ok("Saved %s", p)
		}
		if err != nil && !errors.Is(err, download.ErrFileExists) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (ss *session) info(id string, interactive bool) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	d, err := x.GameDetails(ss.ctx, id)
	if err != nil && !api.IsNotFound(err) && ss.ctx.Err() == nil {
		// The details call fails for some games; the search data and an
		// availability check give the same screen, without the DLC list.
		if g, gerr := x.Game(ss.ctx, id); gerr == nil {
			d, err = api.GameDetails{Game: g}, nil
			if av, aerr := x.Availability(ss.ctx, []string{id}); aerr == nil && len(av) == 1 {
				d.Availability = map[string]bool{"depotbox": av[0].Available}
			}
		}
	}
	if api.IsNotFound(err) {
		ss.ui.fail(ss.out, "AppID %s is not in the DepotBox catalog.", id)
		if interactive {
			return ss.offerRequest(id)
		}
		fmt.Fprintf(ss.out, "  Ask for it with: dbox request %s\n", id)
		return err
	}
	if err != nil {
		return err
	}
	clearScreen(ss.out)
	ss.renderImage(headerImage(d.Game))
	ss.printGameDetails(d.Game)
	if d.Available() {
		ss.ui.field("Available", ss.ui.green("Yes"))
	} else {
		ss.ui.field("Available", ss.ui.red("No"))
	}
	if len(d.DLCs) > 0 {
		ss.ui.field("DLCs", fmt.Sprint(len(d.DLCs)))
		for i, dlc := range d.DLCs {
			if i == 15 {
				fmt.Fprintf(ss.out, "      %s\n", ss.ui.dim(fmt.Sprintf("...and %d more", len(d.DLCs)-i)))
				break
			}
			fmt.Fprintf(ss.out, "      %s %s\n", ss.ui.dim(pad(string(dlc.AppID), 9)), dlc.Name)
		}
	}
	if !interactive {
		if d.Available() {
			fmt.Fprintf(ss.out, "\n  Download with: dbox download %s\n", id)
		}
		return nil
	}
	if !d.Available() {
		return ss.offerRequest(id)
	}
	if !ss.ask("Download?") {
		return nil
	}
	return ss.downloadOne(id, d.Name, true)
}

func (ss *session) check(ids []string) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	for start := 0; start < len(ids); start += 100 {
		res, err := x.Availability(ss.ctx, ids[start:min(start+100, len(ids))])
		if err != nil {
			return err
		}
		for _, r := range res {
			if r.Available {
				ss.ui.ok("%s available", r.AppID)
			} else {
				ss.ui.fail(ss.out, "%s not available", r.AppID)
			}
		}
	}
	return nil
}

// --- fixes ------------------------------------------------------------------

func (ss *session) fixes(query, tags string) error {
	r, err := ss.s.Fixes(ss.ctx, query, tags)
	if err != nil {
		return err
	}
	clearScreen(ss.out)
	return ss.chooseFix(r)
}

func (ss *session) chooseFix(r api.FixesResponse) error {
	if len(r.Fixes) == 0 {
		ss.ui.warn("No fixes found.")
		return nil
	}
	ss.ui.title(fmt.Sprintf("Game fixes (%s)", plural(len(r.Fixes), "fix", "fixes")))
	for i, f := range r.Fixes {
		fmt.Fprintf(ss.out, "%3d. %s %s %s\n", i+1, pad(f.Name, 40), ss.ui.yellow(pad(strings.Join(f.Tags, ","), 12)), ss.ui.dim(f.Size+"  "+f.File))
	}
	i, ok, err := ss.readSelection(len(r.Fixes), "fix")
	if err != nil || !ok {
		return err
	}
	f := r.Fixes[i]
	clearScreen(ss.out)
	ss.ui.title(f.Name)
	ss.ui.field("File", f.File)
	ss.ui.field("Size", orDefault(f.Size, "unknown"))
	ss.ui.field("Tags", orDefault(strings.Join(f.Tags, ", "), "none"))
	if !ss.ask("Download?") {
		return nil
	}
	act := ss.ui.start("Downloading " + f.File)
	p, err := ss.s.DownloadFix(ss.ctx, f)
	summary := act.end()
	if errors.Is(err, download.ErrFileExists) {
		ss.ui.warn("Already downloaded: %s", p)
		return nil
	}
	if err != nil {
		return err
	}
	// A fix patches game files, so it is not a depot ZIP for the handoff target.
	ss.ui.ok("Saved %s %s", p, ss.ui.dim("("+summary+")"))
	return nil
}

// --- community lists --------------------------------------------------------

func (ss *session) browseLists(page int) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	for {
		lp, err := x.Lists(ss.ctx, page)
		if err != nil {
			return err
		}
		clearScreen(ss.out)
		ss.ui.title(fmt.Sprintf("Community lists - page %d of %d", lp.Pagination.CurrentPage, lp.Pagination.TotalPages))
		if len(lp.Lists) == 0 {
			ss.ui.warn("No lists on this page.")
			return nil
		}
		for i, l := range lp.Lists {
			fmt.Fprintf(ss.out, "%3d. %s %s %s\n", i+1, pad(l.Name, 40), pad(plural(l.DepotCount, "game", "games"), 10), ss.ui.dim(pad(plural(l.Likes, "like", "likes"), 9)+" by "+l.Creator))
		}
		fmt.Fprintln(ss.out)
		line, err := ss.prompt(fmt.Sprintf("Open a list (1-%d), n next, p previous, Enter to go back: ", len(lp.Lists)))
		if err != nil || line == "" {
			ss.ui.shown = false // the user chose to go back
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch strings.ToLower(line) {
		case "n":
			if page < lp.Pagination.TotalPages {
				page++
			}
		case "p":
			if page > 1 {
				page--
			}
		default:
			i, err := parseSelection([]string{line}, len(lp.Lists))
			if err != nil {
				continue // the page is drawn again
			}
			if err := ss.showList(lp.Lists[i].ID); err != nil {
				return err
			}
		}
	}
}

func (ss *session) showList(id string) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	l, err := x.List(ss.ctx, id)
	if err != nil {
		return err
	}
	clearScreen(ss.out)
	ss.ui.title(l.Name)
	fmt.Fprintln(ss.out, ss.ui.dim(fmt.Sprintf("by %s, %s", l.Creator, plural(l.Likes, "like", "likes"))))
	if l.Description != "" {
		fmt.Fprintln(ss.out, l.Description)
	}
	fmt.Fprintln(ss.out)
	if len(l.Games) == 0 {
		ss.ui.warn("This list has no games.")
		return nil
	}
	ss.gameRows(l.Games)
	ids := make([]string, 0, len(l.Games))
	for _, g := range l.Games {
		ids = append(ids, string(g.AppID))
	}
	zips := plural((len(ids)+api.MaxBatch-1)/api.MaxBatch, "batch ZIP", "batch ZIPs")
	fmt.Fprintln(ss.out)
	line, err := ss.prompt(fmt.Sprintf("Select a game (1-%d), a to download all as %s, Enter to go back: ", len(ids), zips))
	if err != nil || line == "" {
		ss.ui.shown = false
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if strings.EqualFold(line, "a") {
		fmt.Fprintln(ss.out)
		return ss.downloadBatch(ids, false)
	}
	i, err := parseSelection([]string{line}, len(l.Games))
	if err != nil {
		ss.ui.shown = false
		return nil // back to the lists page
	}
	return ss.openGame(l.Games[i], false)
}

// --- requests and usage -----------------------------------------------------

func (ss *session) request(id string) error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	r, err := x.RequestGame(ss.ctx, id)
	if err != nil {
		return err
	}
	name := r.GameName
	if name == "" {
		name = "AppID " + id
	}
	ss.ui.ok("Requested %s (%s). %s", name, r.Status, r.Message)
	return nil
}

func (ss *session) requests() error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	list, err := x.GameRequests(ss.ctx)
	if err != nil {
		return err
	}
	ss.ui.title("Your game requests")
	if len(list) == 0 {
		ss.ui.warn("No requests yet. Use: dbox request <appid>")
		return nil
	}
	for _, r := range list {
		status := r.Status
		switch status {
		case "accepted":
			status = ss.ui.green(pad(status, 9))
		case "rejected":
			status = ss.ui.red(pad(status, 9))
		default:
			status = ss.ui.yellow(pad(status, 9))
		}
		line := fmt.Sprintf("  %s %s %s %s", r.CreatedAt.Local().Format("2006-01-02"), status, ss.ui.dim(pad(string(r.AppID), 9)), r.GameName)
		if r.RejectionReason != nil && *r.RejectionReason != "" {
			line += ss.ui.dim(" - " + *r.RejectionReason)
		}
		fmt.Fprintln(ss.out, line)
	}
	return nil
}

func (ss *session) usage() error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	clearScreen(ss.out)
	u, err := x.Usage(ss.ctx)
	if err != nil {
		return err
	}
	limit := 60
	if ss.c.Plan == config.Pro {
		limit = 120
	}
	ss.ui.title(fmt.Sprintf("Your API key (%s plan, %d requests a minute)", capitalize(orDefault(string(ss.c.Plan), "unknown")), limit))
	ss.ui.field("Last 24 hours", humanCount(u.Stats.Last24h))
	ss.ui.field("Last 7 days", humanCount(u.Stats.Last7d))
	ss.ui.field("Last 30 days", humanCount(u.Stats.Last30d))
	ss.ui.field("All time", humanCount(u.Stats.AllTime))
	if len(u.TopAppIDs) > 0 {
		var top []string
		for i, t := range u.TopAppIDs {
			if i == 5 {
				break
			}
			top = append(top, fmt.Sprintf("%s (%d)", t.AppID, t.Count))
		}
		ss.ui.field("Top AppIDs", strings.Join(top, ", "))
	}
	for i, r := range u.Recent {
		if i == 5 {
			break
		}
		label := ""
		if i == 0 {
			label = "Recent"
		}
		ss.ui.field(label, fmt.Sprintf("%s  %s", r.At.Local().Format("2006-01-02 15:04"), r.AppID))
	}
	st, err := x.Stats(ss.ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(ss.out)
	ss.ui.title("DepotBox service")
	ss.ui.field("Tracked games", humanCount(st.TrackedGames))
	ss.ui.field("Downloads 24h", humanCount(st.Downloads24h))
	ss.ui.field("API calls 24h", humanCount(st.APIRequests24h))
	if st.MostPopular.Name != "" {
		ss.ui.field("Top today", fmt.Sprintf("%s (%s), %d downloads", st.MostPopular.Name, st.MostPopular.AppID, st.MostPopular.Downloads))
	}
	ss.ui.field("Public lists", humanCount(st.PublicLists))
	return nil
}

func (ss *session) refreshGameList() error {
	x, err := ss.ext()
	if err != nil {
		return err
	}
	act := ss.ui.start("Downloading the DepotBox game list")
	n, err := x.UpdateGameList(ss.ctx)
	summary := act.end()
	if err != nil {
		return err
	}
	ss.ui.ok("Game list updated: %s games %s", humanCount(n), ss.ui.dim("("+strings.TrimPrefix(summary, "0 B in ")+")"))
	return nil
}

// --- config -----------------------------------------------------------------

func (ss *session) configCommand(in io.Reader, edit bool) (config.Config, error) {
	c := ss.c
	ss.printConfig(c)
	if !edit {
		return c, nil
	}
	r, ok := in.(*bufio.Reader)
	if !ok {
		r = bufio.NewReader(in)
	}
	ask := func(prompt string) (string, error) {
		clearScreen(ss.out)
		v, err := readLine(r, ss.out, prompt)
		return strings.TrimSpace(v), err
	}
	for {
		fmt.Fprint(ss.out, "\n  1. API key\n  2. Download directory\n  3. Plan\n  4. Auto handoff\n  5. Preferred Steam library\n  6. Handoff command\n  7. Windows Steam root\n  0. Save and return\n\n")
		choice, err := readLine(r, ss.out, ss.ui.bold("Select a setting: "))
		if err != nil && choice == "" {
			return c, err
		}
		var value string
		switch strings.TrimSpace(choice) {
		case "1":
			clearScreen(ss.out)
			v, e := readAPIKey(r, ss.out)
			if e != nil {
				return c, e
			}
			if v = strings.TrimSpace(v); v != "" {
				c.APIKey = v
			}
		case "2":
			if value, err = ask("New download directory (Enter to keep current): "); err != nil {
				return c, err
			}
			if value != "" {
				c.DownloadDir = expandHome(value)
			}
		case "3":
			if value, err = ask("New plan (starter/pro, Enter to keep current): "); err != nil {
				return c, err
			}
			if value != "" {
				c.Plan = config.Plan(strings.ToLower(value))
			}
		case "4":
			if value, err = ask("Auto handoff (off/accela/steam, Enter to keep current): "); err != nil {
				return c, err
			}
			if value != "" {
				c.HandoffTarget = config.HandoffTarget(strings.ToLower(value))
			}
		case "5":
			if value, err = ask("Preferred Steam library (Enter for Accela to choose): "); err != nil {
				return c, err
			}
			c.HandoffLibrary = value
		case "6":
			if value, err = ask("Handoff command (Enter for accela): "); err != nil {
				return c, err
			}
			c.HandoffCommand = value
		case "7":
			if value, err = ask("Steam root (folder with steam.exe or steamapps, Enter to auto-detect local Steam): "); err != nil {
				return c, err
			}
			if value != "" {
				c.SteamRoot = value
			}
		case "0", "s", "d", "done":
			if err := saveConfig(c); err != nil {
				return ss.c, err
			}
			clearScreen(ss.out)
			ss.ui.ok("Configuration saved")
			return c, nil
		default:
			ss.ui.fail(ss.out, "invalid setting")
		}
		clearScreen(ss.out)
		ss.printConfig(c)
	}
}

func (ss *session) printConfig(c config.Config) {
	target := orDefault(string(c.HandoffTarget), string(config.HandoffNone))
	ss.ui.title("Settings")
	if p, err := config.DefaultPath(); err == nil {
		ss.ui.field("Config file", p)
	}
	ss.ui.field("API key", "configured")
	ss.ui.field("Download dir", c.DownloadDir)
	ss.ui.field("Plan", string(c.Plan))
	ss.ui.field("Auto handoff", target)
	if c.HandoffTarget == config.HandoffAccela {
		ss.ui.field("Steam library", orDefault(c.HandoffLibrary, "Accela chooses"))
		ss.ui.field("Handoff command", orDefault(c.HandoffCommand, "accela"))
	}
	ss.ui.field("Steam root", orDefault(c.SteamRoot, "auto-detect"))
}

func saveConfig(c config.Config) error {
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
	return config.Save(p, c)
}

// --- history ----------------------------------------------------------------

// --- installed games --------------------------------------------------------

// installed lists the games in the Steam libraries. It reads Steam's own
// appmanifest files, so games that Steam installed show up next to the ones a
// handoff put there.
func (ss *session) installed() error {
	lister, ok := ss.s.(installedLister)
	if !ok {
		ss.ui.warn("Installed games are unavailable.")
		return nil
	}
	apps, err := lister.InstalledApps()
	if err != nil {
		return err
	}
	ss.ui.title("Installed games")
	if len(apps) == 0 {
		ss.ui.warn("No Steam games found.")
		return nil
	}
	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	managed := 0
	for _, a := range apps {
		source, size := ss.ui.dim("steam  "), humanBytes(a.SizeOnDisk)
		if a.Managed {
			source = ss.ui.green("managed")
			managed++
		}
		if !a.Installed {
			size = ss.ui.dim("not installed")
		}
		fmt.Fprintf(ss.out, "  %s %s %s %8s %s\n", ss.ui.dim(pad(a.AppID, 8)), pad(a.Name, 44), source, size, ss.ui.dim(a.Library))
	}
	fmt.Fprintln(ss.out)
	ss.ui.ok("%s, %s managed by dbox or a client mod. Steam handles updates and removal.", plural(len(apps), "game", "games"), humanCount(managed))
	return nil
}

// showHistory lists recent actions, newest first. limit 0 shows all.
func (ss *session) showHistory(query string, limit int) error {
	lister, ok := ss.s.(historyLister)
	if !ok {
		ss.ui.warn("History is unavailable.")
		return nil
	}
	entries, err := lister.HistoryEntries()
	if err != nil {
		return err
	}
	var match []history.Entry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if query == "" || strings.Contains(strings.ToLower(e.Query+" "+e.Name+" "+e.AppID), strings.ToLower(query)) {
			match = append(match, e)
		}
	}
	ss.ui.title("History")
	if len(match) == 0 {
		ss.ui.warn("No history yet.")
		return nil
	}
	shown := match
	if limit > 0 && len(match) > limit {
		shown = match[:limit]
	}
	for _, e := range shown {
		mark := ss.ui.green(ss.ui.sym("✓", "ok"))
		switch e.Status {
		case "failed":
			mark = ss.ui.red(ss.ui.sym("✗", "!!"))
		case "exists":
			mark = ss.ui.yellow(ss.ui.sym("•", "--"))
		}
		subject := e.Query
		if e.Path != "" {
			subject = filepath.Base(e.Path)
		} else if e.Name != "" {
			subject = e.Name
		}
		fmt.Fprintf(ss.out, "%s %s  %s %s %s\n", mark, ss.ui.dim(e.At.Local().Format("2006-01-02 15:04")), pad(e.Kind, 12), ss.ui.dim(pad(e.AppID, 9)), pad(subject, 60))
	}
	if len(shown) < len(match) {
		fmt.Fprintln(ss.out)
		fmt.Fprintln(ss.out, ss.ui.dim(fmt.Sprintf("Showing the latest %d of %d. Run dbox history to see all, or dbox history <term> to filter.", len(shown), len(match))))
	}
	return nil
}

// --- terminal helpers -------------------------------------------------------

// renderImage shows a header image with chafa when it is installed and the output is a terminal.
func (ss *session) renderImage(imageURL string) {
	if imageURL == "" || !isTTY(ss.out) {
		return
	}
	if _, err := exec.LookPath("chafa"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ss.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
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
	if _, err = io.Copy(tmp, io.LimitReader(resp.Body, 8<<20)); err != nil {
		tmp.Close()
		return
	}
	if err = tmp.Close(); err != nil {
		return
	}
	cmd := exec.CommandContext(ctx, "chafa", "--format=symbols", "--size=60x15", filepath.Clean(name))
	if preview, err := cmd.Output(); err == nil {
		fmt.Fprintln(ss.out, string(preview))
	}
}

// clearScreen clears the terminal. It writes nothing into a file or pipe.
func clearScreen(out io.Writer) {
	if f, ok := out.(*os.File); ok && !isTerminal(f) {
		return
	}
	fmt.Fprint(out, "\033[2J\033[H")
}

func splitIDs(text string) []string {
	return appIDsFrom(strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }))
}

// readSelection reads a 1-based choice. ok is false when the user leaves it blank.
func (ss *session) readSelection(n int, what string) (int, bool, error) {
	fmt.Fprintln(ss.out)
	line, err := ss.prompt(fmt.Sprintf("Select a %s (1-%d, Enter to go back): ", what, n))
	if line == "" {
		ss.ui.shown = false // the user chose to go back
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return 0, false, err
	}
	i, err := parseSelection([]string{line}, n)
	return i, err == nil, err
}

func readInputLine(in io.Reader) (string, error) {
	if r, ok := in.(*bufio.Reader); ok && lineEdit {
		return editLine(r, os.Stdout)
	}
	return plainLine(in)
}

func plainLine(in io.Reader) (string, error) {
	if r, ok := in.(*bufio.Reader); ok {
		line, err := r.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err
	}
	// Read one byte at a time so the rest of the input stays for the next reader.
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
		return 0, fmt.Errorf("invalid selection %q (pick 1-%d)", strings.TrimSpace(lines[0]), n)
	}
	return i - 1, nil
}

// yes reads a y/n answer. "No" means go back, so the menu does not pause after it.
func (ss *session) yes() bool {
	line, _ := readInputLine(ss.in)
	ok := strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
	if !ok {
		ss.ui.shown = false
	}
	return ok
}
