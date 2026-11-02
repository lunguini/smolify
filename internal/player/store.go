package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// logger writes the player's info, warnings and errors to w, or discards
// them when w is nil: in the app, failures reach the UI as events and
// request errors instead.
type logger struct {
	w      io.Writer
	fields string
}

func (l logger) out(level, msg string) {
	if l.w != nil {
		fmt.Fprintf(l.w, "%s %s%s\n", level, msg, l.fields)
	}
}

func (l logger) Tracef(string, ...any)     {}
func (l logger) Debugf(string, ...any)     {}
func (l logger) Infof(f string, a ...any)  { l.out("info ", fmt.Sprintf(f, a...)) }
func (l logger) Warnf(f string, a ...any)  { l.out("warn ", fmt.Sprintf(f, a...)) }
func (l logger) Errorf(f string, a ...any) { l.out("error", fmt.Sprintf(f, a...)) }
func (l logger) Trace(...any)              {}
func (l logger) Debug(...any)              {}
func (l logger) Info(a ...any)             { l.out("info ", fmt.Sprint(a...)) }
func (l logger) Warn(a ...any)             { l.out("warn ", fmt.Sprint(a...)) }
func (l logger) Error(a ...any)            { l.out("error", fmt.Sprint(a...)) }
func (l logger) WithField(k string, v any) librespot.Logger {
	l.fields += fmt.Sprintf(" %s=%v", k, v)
	return l
}
func (l logger) WithError(err error) librespot.Logger { return l.WithField("error", err) }
