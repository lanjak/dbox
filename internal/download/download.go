package download

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Downloader struct{}

func New() *Downloader { return &Downloader{} }

func (d *Downloader) SaveStream(dir, filename string, src io.Reader, overwrite bool) (string, error) {
	return SaveStream(dir, filename, src, overwrite)
}

func SanitizeFilename(name string) string {
	name = filepath.Base(name)
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == '?' || r == '*' || r == '"' || r == '<' || r == '>' || r == '|' || r < 32 {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	if name == "" || name == "." {
		return "download.zip"
	}
	return name
}

func SaveStream(dir, filename string, src io.Reader, overwrite bool) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	filename = SanitizeFilename(filename)
	final := filepath.Join(dir, filename)
	if !overwrite {
		if _, err := os.Stat(final); err == nil {
			return "", fmt.Errorf("file already exists: %s", filename)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	tmp, err := os.CreateTemp(dir, ".dbox-")
	if err != nil {
		return "", err
	}
	tmpname := tmp.Name()
	defer os.Remove(tmpname)
	if _, err = io.Copy(tmp, src); err != nil {
		tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if overwrite {
		if err = os.Rename(tmpname, final); err != nil {
			return "", err
		}
	} else {
		if err = os.Link(tmpname, final); err != nil {
			if os.IsExist(err) {
				return "", fmt.Errorf("file already exists: %s", filename)
			}
			return "", err
		}
		if err = os.Remove(tmpname); err != nil {
			return "", err
		}
	}
	return final, nil
}
