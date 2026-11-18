package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Plan string

type HandoffTarget string

const (
	Starter             Plan          = "starter"
	Pro                 Plan          = "pro"
	HandoffNone         HandoffTarget = "off"
	HandoffAccela       HandoffTarget = "accela"
	HandoffWindowsSteam HandoffTarget = "windows-steam"
)

type Config struct {
	APIKey         string        `json:"api_key"`
	DownloadDir    string        `json:"download_dir"`
	Plan           Plan          `json:"plan"`
	HandoffTarget  HandoffTarget `json:"handoff_target,omitempty"`
	HandoffLibrary string        `json:"handoff_library,omitempty"`
	HandoffCommand string        `json:"handoff_command,omitempty"`
	SteamRoot      string        `json:"steam_root,omitempty"`
}

func New() Config { return Config{} }

func (c Config) Validate() error {
	if c.APIKey == "" {
		return errors.New("API key is required")
	}
	if c.DownloadDir == "" {
		return errors.New("download directory is required")
	}
	if c.Plan != Starter && c.Plan != Pro {
		return fmt.Errorf("unsupported plan %q", c.Plan)
	}
	if c.HandoffTarget == "" {
		return nil
	}
	if c.HandoffTarget != HandoffNone && c.HandoffTarget != HandoffAccela && c.HandoffTarget != HandoffWindowsSteam {
		return fmt.Errorf("unsupported handoff target %q", c.HandoffTarget)
	}
	if c.HandoffTarget == HandoffWindowsSteam && c.SteamRoot == "" {
		return errors.New("steam_root is required for windows-steam handoff")
	}
	return nil
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := openConfigFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	_, err = f.Write(b)
	return err
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dbox", "config.json"), nil
}

func HistoryPath() (string, error) {
	p, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "history.json"), nil
}
