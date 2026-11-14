//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"syscall/js"

	"github.com/lunguini/smolify/internal/app"
	"github.com/lunguini/smolify/internal/webspotify"
)

const (
	tokenKey    = "smolify.spotify.tokens.v1"
	pendingKey  = "smolify.spotify.pkce.v1"
	modeKey     = "smolify.spotify.mode"
	settingsKey = "smolify.settings.v1"
)

type browserSessionStore struct{}

func (browserSessionStore) LoadToken() (webspotify.Token, error) {
	var token webspotify.Token
	value, err := browserStorageGet("sessionStorage", tokenKey)
	if err != nil || value == "" {
		return token, err
	}
	err = json.Unmarshal([]byte(value), &token)
	return token, err
}

func (browserSessionStore) SaveToken(token webspotify.Token) error {
	value, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return browserStorageSet("sessionStorage", tokenKey, string(value))
}

func (browserSessionStore) ClearToken() error {
	if err := browserStorageRemove("sessionStorage", tokenKey); err != nil {
		return err
	}
	return browserStorageRemove("sessionStorage", pendingKey)
}

func (browserSessionStore) SavePending(pending webspotify.PendingAuth) error {
	value, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return browserStorageSet("sessionStorage", pendingKey, string(value))
}

func (browserSessionStore) LoadPending() (webspotify.PendingAuth, error) {
	var pending webspotify.PendingAuth
	value, err := browserStorageGet("sessionStorage", pendingKey)
	if err != nil || value == "" {
		return pending, err
	}
	err = json.Unmarshal([]byte(value), &pending)
	return pending, err
}

func (browserSessionStore) ClearPending() error {
	return browserStorageRemove("sessionStorage", pendingKey)
}

func (browserSessionStore) SetSpotifyMode(enabled bool) error {
	if enabled {
		return browserStorageSet("sessionStorage", modeKey, "true")
	}
	return browserStorageRemove("sessionStorage", modeKey)
}

func (browserSessionStore) SpotifyMode() bool {
	value, _ := browserStorageGet("sessionStorage", modeKey)
	return value == "true"
}

type browserSettingsStore struct{}

func (browserSettingsStore) Save(settings app.Settings) error {
	value, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return browserStorageSet("localStorage", settingsKey, string(value))
}

func loadBrowserSettings() app.Settings {
	settings := app.DefaultSettings()
	value, err := browserStorageGet("localStorage", settingsKey)
	if err == nil && value != "" {
		if json.Unmarshal([]byte(value), &settings) != nil {
			settings = app.DefaultSettings()
		}
	}
	if settings.DeviceName == "" {
		settings.DeviceName = app.DefaultSettings().DeviceName
	}
	return settings
}

func browserStorageGet(which, key string) (value string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("browser storage read failed: %v", recovered)
		}
	}()
	storage, err := browserStorage(which)
	if err != nil {
		return "", err
	}
	result := storage.Call("getItem", key)
	if result.IsNull() || result.IsUndefined() {
		return "", nil
	}
	return result.String(), nil
}

func browserStorageSet(which, key, value string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("browser storage write failed: %v", recovered)
		}
	}()
	storage, err := browserStorage(which)
	if err != nil {
		return err
	}
	storage.Call("setItem", key, value)
	return nil
}

func browserStorageRemove(which, key string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("browser storage remove failed: %v", recovered)
		}
	}()
	storage, err := browserStorage(which)
	if err != nil {
		return err
	}
	storage.Call("removeItem", key)
	return nil
}

func browserStorage(which string) (js.Value, error) {
	window := js.Global().Get("window")
	if window.IsUndefined() || window.IsNull() {
		return js.Undefined(), errors.New("browser window is unavailable")
	}
	storage := window.Get(which)
	if storage.IsUndefined() || storage.IsNull() {
		return js.Undefined(), errors.New("browser storage is unavailable")
	}
	return storage, nil
}
