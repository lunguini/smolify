//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"syscall/js"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/window"
	"github.com/lunguini/smolify/internal/app"
	"github.com/lunguini/smolify/internal/demo"
	"github.com/lunguini/smolify/internal/librarycache"
	"github.com/lunguini/smolify/internal/webimages"
	"github.com/lunguini/smolify/internal/webplayer"
	"github.com/lunguini/smolify/internal/webspotify"
)

func main() {
	ctx := context.Background()
	settings := loadBrowserSettings()
	session := browserSessionStore{}
	query := currentQuery()

	if query.Has("spotify") {
		_ = session.SetSpotifyMode(true)
	}
	if query.Has("demo") {
		_ = session.SetSpotifyMode(false)
		clearQuery()
		startDemo(ctx, settings)
		return
	}
	if !query.Has("spotify") && !session.SpotifyMode() {
		startDemo(ctx, settings)
		return
	}

	clientID := configValue("clientId", "smolify-client-id")
	redirectURI := configValue("redirectUri", "smolify-redirect-uri")
	if clientID == "" {
		_ = session.SetSpotifyMode(false)
		clearQuery()
		setStatus("Demo mode · configure a client ID to connect an account")
		startDemo(ctx, settings)
		return
	}
	if redirectURI == "" {
		location := js.Global().Get("window").Get("location")
		redirectURI = location.Get("origin").String() + location.Get("pathname").String()
	}
	auth := &webspotify.Auth{
		ClientID: clientID, RedirectURI: redirectURI,
		Transport: webspotify.FetchTransport{}, Store: session, Random: webspotify.WebCryptoReader{},
	}

	if authorizationErr := query.Get("error"); authorizationErr != "" {
		_ = session.SetSpotifyMode(false)
		_ = session.ClearPending()
		clearQuery()
		setStatus("Account connection was not completed · showing demo mode")
		startDemo(ctx, settings)
		return
	}
	if code := query.Get("code"); code != "" {
		if err := auth.ExchangeCode(ctx, code, query.Get("state")); err != nil {
			_ = session.SetSpotifyMode(false)
			clearQuery()
			setStatus("Could not finish account sign-in: " + err.Error())
			startDemo(ctx, settings)
			return
		}
		clearQuery()
	}
	if _, err := auth.AccessToken(ctx); err != nil {
		if errors.Is(err, webspotify.ErrNoToken) {
			beginSignIn(session, auth)
			return
		}
		_ = session.ClearToken()
		_ = session.SetSpotifyMode(false)
		clearQuery()
		setStatus("Could not refresh the account session: " + err.Error())
		startDemo(ctx, settings)
		return
	}
	clearQuery()

	api := webspotify.NewAPI(auth, webspotify.FetchTransport{})
	sdk := webplayer.New(settings.DeviceName, auth.AccessToken)
	backend := webspotify.NewClient(api, auth, sdk)
	go func() {
		if err := backend.Start(ctx); err != nil {
			setStatus("Browser playback could not start: " + err.Error())
			return
		}
		setStatus("Account mode · browser playback connected")
	}()
	setStatus("Account mode · connecting browser playback")
	a := app.New(backend, webimages.Images{}, backend, nil)
	a = app.WithSettingsStore(a, browserSettingsStore{}, settings)
	a = app.WithCacheStore(a, librarycache.New(""))
	if err := runBrowserApp(ctx, a); err != nil {
		setStatus("Smolify could not start: " + err.Error())
	}
}

func startDemo(ctx context.Context, settings app.Settings) {
	backend := demo.New()
	go backend.Run(ctx)
	setStatus("Demo mode · sample data and simulated playback")
	a := app.New(backend, backend, backend, nil)
	a = app.WithSettingsStore(a, browserSettingsStore{}, settings)
	if err := runBrowserApp(ctx, a); err != nil {
		setStatus("Smolify could not start: " + err.Error())
	}
}

const browserViewKey = "smolify.web.view"

func runBrowserApp(ctx context.Context, a flatte.App[app.State]) error {
	options := []flatte.Option{
		flatte.WithMouse(flatte.MouseModeCellMotion),
		flatte.WithoutDefaultQuit(),
		flatte.WithReportFocus(),
	}
	if browserGUIEnabled() {
		a = app.Windowed(a)
		theme := app.WindowTheme()
		options = append(options, window.Options{Title: "Smolify", Theme: &theme, Fonts: app.WindowFonts()})
	}
	return flatte.Run(ctx, a, options...)
}

func browserGUIEnabled() bool {
	value, err := browserStorageGet("localStorage", browserViewKey)
	return err != nil || value != "terminal"
}

func beginSignIn(session browserSessionStore, auth *webspotify.Auth) {
	address, err := auth.Begin()
	if err != nil {
		_ = session.SetSpotifyMode(false)
		setStatus("Could not start account sign-in: " + err.Error())
		startDemo(context.Background(), loadBrowserSettings())
		return
	}
	_ = session.SetSpotifyMode(true)
	setStatus("Opening account sign-in…")
	js.Global().Get("window").Get("location").Set("href", address)
}

func currentQuery() url.Values {
	location := js.Global().Get("window").Get("location")
	query, _ := url.ParseQuery(strings.TrimPrefix(location.Get("search").String(), "?"))
	return query
}

func clearQuery() {
	window := js.Global().Get("window")
	location := window.Get("location")
	path := location.Get("pathname").String() + location.Get("hash").String()
	window.Get("history").Call("replaceState", js.Null(), "", path)
}

func configValue(globalName, metaName string) string {
	global := js.Global().Get("window").Get("SmolifyConfig")
	if !global.IsUndefined() && !global.IsNull() {
		value := global.Get(globalName)
		if !value.IsUndefined() && !value.IsNull() && strings.TrimSpace(value.String()) != "" {
			return strings.TrimSpace(value.String())
		}
	}
	doc := js.Global().Get("document")
	meta := doc.Call("querySelector", `meta[name="`+metaName+`"]`)
	if meta.IsUndefined() || meta.IsNull() {
		return ""
	}
	return strings.TrimSpace(meta.Get("content").String())
}

func setStatus(message string) {
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.IsNull() {
		return
	}
	status := doc.Call("getElementById", "mode-status")
	if !status.IsUndefined() && !status.IsNull() {
		status.Set("textContent", message)
	}
}
