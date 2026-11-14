//go:build !js || !wasm

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lunguini/flatte"
)

// SettingsPath is where settings live in dir (the player's state dir).
func SettingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

// LoadSettings reads the settings at path, using defaults when no file exists.
func LoadSettings(path string) (Settings, error) {
	st := DefaultSettings()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return DefaultSettings(), err
	}
	if strings.TrimSpace(st.DeviceName) == "" {
		st.DeviceName = DefaultSettings().DeviceName
	}
	return st, nil
}

func saveSettings(path string, st Settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// settingsWriter serializes disk writes and skips snapshots superseded before
// their worker starts. Revisions are assigned on the app loop.
type settingsWriter struct {
	path   string
	latest atomic.Uint64
	mu     sync.Mutex
}

func (w *settingsWriter) prepare(st Settings) func() error {
	revision := w.latest.Add(1)
	return func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		if revision != w.latest.Load() {
			return nil
		}
		return saveSettings(w.path, st)
	}
}

type fileSettingsStore struct{ writer *settingsWriter }

func (s *fileSettingsStore) Save(st Settings) error { return s.writer.prepare(st)() }

// WithSettings preserves the native path-based setup for existing callers.
func WithSettings(a flatte.App[State], path string, st Settings) flatte.App[State] {
	var store SettingsStore
	if path != "" {
		store = &fileSettingsStore{writer: &settingsWriter{path: path}}
	}
	return WithSettingsStore(a, store, st)
}
