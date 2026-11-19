//go:build !js || !wasm

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// ThemesPath is where the Marketplace themes chosen lately are kept in dir
// (beside the settings).
func ThemesPath(dir string) string { return filepath.Join(dir, "themes.json") }

// LoadThemes reads the themes kept at path; none when there is no file.
func LoadThemes(path string) ([]SavedTheme, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var themes []SavedTheme
	if err := json.Unmarshal(b, &themes); err != nil {
		return nil, err
	}
	return themes, nil
}

// FileThemes keeps the chosen themes in a file, replaced whole.
type FileThemes struct {
	Path string
	mu   sync.Mutex
}

// SaveThemes writes themes to f.Path.
func (f *FileThemes) SaveThemes(themes []SavedTheme) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := json.Marshal(themes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".themes-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}
