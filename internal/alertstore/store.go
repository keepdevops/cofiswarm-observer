// Package alertstore persists the observer's alert history to a JSON file so it
// survives a restart. Alerts are low-volume incident records (KV-budget gate
// denials, model evictions, mode-unavailable, …); the live roster is NOT
// persisted because it self-heals from announce/hello on startup.
//
// JSON file (not sqlite) keeps the observer CGO-free (it builds with
// CGO_ENABLED=0) and matches the project's existing file-state pattern
// (dispatch sessions.json / history.json).
package alertstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/keepdevops/cofiswarm-observer/internal/bustail"
)

// DefaultMax is the retention cap (most-recent alerts kept on disk). The in-memory
// ring is smaller; the file keeps more history for an operator to inspect.
const DefaultMax = 1000

// Store is a JSON-file-backed, retention-capped alert log. Safe for concurrent use.
type Store struct {
	path string
	max  int
	mu   sync.Mutex
	buf  []bustail.Alert
}

// New opens (or initializes) the store at path, loading any existing alerts.
// A missing file is fine (fresh start); a corrupt file is a loud error.
func New(path string, max int) (*Store, error) {
	if max <= 0 {
		max = DefaultMax
	}
	s := &Store{path: path, max: max}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // fresh start
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	var a []bustail.Alert
	if err := json.Unmarshal(b, &a); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if len(a) > s.max {
		a = a[len(a)-s.max:]
	}
	s.buf = a
	return nil
}

// Existing returns a copy of the loaded alerts (used to seed the in-memory ring).
func (s *Store) Existing() []bustail.Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bustail.Alert(nil), s.buf...)
}

// Append records one alert and persists, trimming to the retention cap.
func (s *Store) Append(a bustail.Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, a)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	return s.persist()
}

// Clear drops all persisted alerts.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = nil
	return s.persist()
}

// persist writes the buffer atomically (temp file + rename) so a crash mid-write
// never leaves a truncated JSON file.
func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(s.path), err)
	}
	b, err := json.MarshalIndent(s.buf, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename %s: %w", s.path, err)
	}
	return nil
}
