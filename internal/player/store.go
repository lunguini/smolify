package player

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	librespot "github.com/devgianlu/go-librespot"
)

// fileStore keeps the player's state — its device ID and stored
// credentials — in dir/state.json, readable by the user only.
type fileStore struct{ dir string }

func (f fileStore) path() string { return filepath.Join(f.dir, "state.json") }

func (f fileStore) Load() (*librespot.AppState, error) {
	st := &librespot.AppState{}
	b, err := os.ReadFile(f.path())
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	return st, json.Unmarshal(b, st)
}

// Save writes atomically, so a crash never leaves half a state file.
func (f fileStore) Save(st *librespot.AppState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path())
}

// SignOut forgets the stored credentials in dir, so the next Start pairs
// again. The device ID is kept.
func SignOut(dir string) error {
	f := fileStore{dir: dir}
	st, err := f.Load()
	if err != nil {
		return err
	}
	st.Credentials.Username, st.Credentials.Data = "", nil
	return f.Save(st)
}

// quietLogger discards the player's log; failures reach the UI as events
// and request errors instead.
type quietLogger struct{}

func (quietLogger) Tracef(string, ...any)                    {}
func (quietLogger) Debugf(string, ...any)                    {}
func (quietLogger) Infof(string, ...any)                     {}
func (quietLogger) Warnf(string, ...any)                     {}
func (quietLogger) Errorf(string, ...any)                    {}
func (quietLogger) Trace(...any)                             {}
func (quietLogger) Debug(...any)                             {}
func (quietLogger) Info(...any)                              {}
func (quietLogger) Warn(...any)                              {}
func (quietLogger) Error(...any)                             {}
func (q quietLogger) WithField(string, any) librespot.Logger { return q }
func (q quietLogger) WithError(error) librespot.Logger       { return q }
