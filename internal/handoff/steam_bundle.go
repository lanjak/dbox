package handoff

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	luaName      = regexp.MustCompile(`^[1-9][0-9]*(?:_[0-9]+)?\.lua$`)
	manifestName = regexp.MustCompile(`^[0-9]+_[0-9]+\.manifest$`)
)

type steamEntry struct {
	name string
	data []byte
	lua  bool
}

// readSteamBundle validates a DepotBox ZIP and returns its entries and AppID.
// Nothing is written here, so a rejected ZIP leaves the Steam folder untouched.
func readSteamBundle(archivePath string) ([]steamEntry, string, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, "", fmt.Errorf("open ZIP: %w", err)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > 128 {
		return nil, "", errors.New("ZIP must have 1 to 128 entries")
	}

	var entries []steamEntry
	seen := make(map[string]bool)
	var total uint64
	var appID string
	for _, f := range archive.File {
		name := f.Name
		normalized := strings.ToLower(name)
		if name != filepath.Base(name) || strings.ContainsAny(name, `/\:`) || seen[normalized] {
			return nil, "", fmt.Errorf("unsafe or duplicate ZIP entry %q", name)
		}
		seen[normalized] = true
		entry := strings.ToLower(normalized)
		isLua := luaName.MatchString(entry)
		if !isLua && !manifestName.MatchString(entry) {
			return nil, "", fmt.Errorf("unexpected ZIP entry %q", name)
		}
		limit := uint64(64 << 20)
		if isLua {
			limit = 1 << 20
			if appID != "" {
				return nil, "", errors.New("ZIP contains multiple Lua configurations")
			}
			appID = strings.SplitN(normalized, "_", 2)[0]
			appID = strings.TrimSuffix(appID, ".lua")
		}
		total += f.UncompressedSize64
		if f.UncompressedSize64 > limit || total > 256<<20 {
			return nil, "", fmt.Errorf("ZIP entry too large: %q", name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, "", fmt.Errorf("read %q: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || uint64(len(data)) != f.UncompressedSize64 || uint64(len(data)) > limit {
			return nil, "", fmt.Errorf("invalid ZIP entry %q", name)
		}
		if isLua && len(bytes.TrimSpace(data)) == 0 {
			return nil, "", fmt.Errorf("empty Lua configuration %q", name)
		}
		entries = append(entries, steamEntry{name: entry, data: data, lua: isLua})
	}
	if appID == "" {
		return nil, "", errors.New("ZIP has no AppID Lua configuration")
	}
	return entries, appID, nil
}

// writeSteamBundle places the manifests in depotDir and the Lua script in
// plugDir. Every manifest is checked against existing files before anything is
// written, and a different manifest under the same content-addressed name is
// never replaced.
func writeSteamBundle(entries []steamEntry, appID, plugDir, depotDir string) error {
	for _, e := range entries {
		if e.lua {
			continue
		}
		existing, err := os.ReadFile(filepath.Join(depotDir, e.name))
		if err == nil && !bytes.Equal(existing, e.data) {
			return fmt.Errorf("manifest %s exists with different content", e.name)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, dir := range []string{plugDir, depotDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	for _, e := range entries {
		if e.lua {
			continue
		}
		f, err := os.OpenFile(filepath.Join(depotDir, e.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := f.Write(e.data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return fmt.Errorf("write manifest %s: %v %v", e.name, writeErr, closeErr)
		}
	}
	for _, e := range entries {
		if !e.lua {
			continue
		}
		return installLua(plugDir, appID, e.data)
	}
	return nil
}

// installLua writes the Lua script, after moving any earlier one to a backup.
func installLua(plugDir, appID string, data []byte) error {
	dest := filepath.Join(plugDir, appID+".lua")
	old, err := os.ReadFile(dest)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	tmp, err := os.CreateTemp(plugDir, ".dbox-*.lua")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	backup := ""
	if old != nil {
		backup = fmt.Sprintf("%s.bak-%s", dest, time.Now().UTC().Format("20060102T150405.000000000Z"))
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("back up existing Lua: %w", err)
		}
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dest)
		}
		return fmt.Errorf("install Lua: %w", err)
	}
	return nil
}
