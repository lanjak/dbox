package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Query  string    `json:"query,omitempty"`
	AppID  string    `json:"appid,omitempty"`
	Name   string    `json:"name,omitempty"`
	Path   string    `json:"path,omitempty"`
	Status string    `json:"status"`
}

type Store struct {
	Path  string
	Limit int
}

func New(path string, limit int) *Store {
	if limit <= 0 {
		limit = 100
	}
	return &Store{Path: path, Limit: limit}
}

func (s *Store) Load() ([]Entry, error) {
	b, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) Append(entry Entry) error {
	entries, err := s.Load()
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	if len(entries) > s.Limit {
		entries = entries[len(entries)-s.Limit:]
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}
