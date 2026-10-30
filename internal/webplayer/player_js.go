//go:build js && wasm

// Package webplayer bridges Spotify's documented Web Playback SDK to Go.
package webplayer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/lunguini/smolify/internal/model"
	"github.com/lunguini/smolify/internal/webspotify"
)

const sdkURL = "https://sdk.scdn.co/spotify-player.js"

type listener struct {
	event string
	fn    js.Func
}

// Player owns the SDK object and its JS callbacks.
type Player struct {
	mu        sync.Mutex
	name      string
	getToken  func(context.Context) (string, error)
	ctx       context.Context
	player    js.Value
	events    chan webspotify.SDKEvent
	listeners []listener
	tokenFn   js.Func
	connected bool
}

func New(name string, getToken func(context.Context) (string, error)) *Player {
	return &Player{name: name, getToken: getToken, events: make(chan webspotify.SDKEvent, 64)}
}

func (p *Player) Events() <-chan webspotify.SDKEvent { return p.events }

func (p *Player) Connect(ctx context.Context) error {
	p.mu.Lock()
	if p.connected {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	if err := loadSDK(ctx); err != nil {
		return err
	}
	window := js.Global().Get("window")
	spotify := window.Get("Spotify")
	ctor := spotify.Get("Player")
	if ctor.IsUndefined() || ctor.IsNull() {
		return errors.New("Spotify Web Playback SDK did not initialize")
	}
	p.ctx = ctx
	options := js.Global().Get("Object").New()
	options.Set("name", p.name)
	options.Set("volume", 0.4)
	p.tokenFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || p.getToken == nil {
			return nil
		}
		callback := args[0]
		go func() {
			token, err := p.getToken(ctx)
			if err != nil {
				p.emit(webspotify.SDKEvent{Type: webspotify.SDKAuthError, Message: err.Error()})
				return
			}
			callback.Invoke(token)
		}()
		return nil
	})
	options.Set("getOAuthToken", p.tokenFn)
	p.player = ctor.New(options)
	p.bindEvents()
	promise := p.player.Call("connect")
	connected, err := waitBoolean(ctx, promise)
	if err != nil {
		return err
	}
	if !connected {
		return errors.New("Spotify Web Playback SDK could not connect")
	}
	p.mu.Lock()
	p.connected = true
	p.mu.Unlock()
	return nil
}

func loadSDK(ctx context.Context) error {
	window := js.Global().Get("window")
	if !window.IsUndefined() && !window.IsNull() {
		spotify := window.Get("Spotify")
		if !spotify.IsUndefined() && !spotify.IsNull() && !spotify.Get("Player").IsUndefined() {
			return nil
		}
	}
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.IsNull() {
		return errors.New("browser document is unavailable")
	}
	done := make(chan error, 1)
	script := doc.Call("createElement", "script")
	loaded := js.FuncOf(func(js.Value, []js.Value) any {
		done <- nil
		return nil
	})
	failed := js.FuncOf(func(js.Value, []js.Value) any {
		done <- errors.New("could not load Spotify Web Playback SDK")
		return nil
	})
	script.Set("src", sdkURL)
	script.Set("async", true)
	script.Set("onload", loaded)
	script.Set("onerror", failed)
	doc.Get("head").Call("appendChild", script)
	select {
	case err := <-done:
		loaded.Release()
		failed.Release()
		return err
	case <-ctx.Done():
		go func() {
			<-done
			loaded.Release()
			failed.Release()
		}()
		return ctx.Err()
	}
}

func (p *Player) bindEvents() {
	p.listen("ready", func(args []js.Value) {
		id := ""
		if len(args) > 0 {
			id = propertyString(args[0], "device_id")
		}
		p.emit(webspotify.SDKEvent{Type: webspotify.SDKReady, DeviceID: id})
	})
	p.listen("not_ready", func(args []js.Value) {
		id := ""
		if len(args) > 0 {
			id = propertyString(args[0], "device_id")
		}
		p.emit(webspotify.SDKEvent{Type: webspotify.SDKNotReady, DeviceID: id})
	})
	p.listen("player_state_changed", func(args []js.Value) {
		if len(args) == 0 || args[0].IsNull() || args[0].IsUndefined() {
			p.emit(webspotify.SDKEvent{Type: webspotify.SDKStateChanged, State: webspotify.SDKState{Empty: true}})
			return
		}
		p.emit(webspotify.SDKEvent{Type: webspotify.SDKStateChanged, State: stateFromJS(args[0])})
	})
	for _, event := range []string{webspotify.SDKInitError, webspotify.SDKAuthError, webspotify.SDKAccountError, webspotify.SDKPlaybackError} {
		kind := event
		p.listen(kind, func(args []js.Value) {
			message := ""
			if len(args) > 0 {
				message = propertyString(args[0], "message")
			}
			p.emit(webspotify.SDKEvent{Type: kind, Message: message})
		})
	}
	p.listen(webspotify.SDKAutoplayFailed, func([]js.Value) {
		p.emit(webspotify.SDKEvent{Type: webspotify.SDKAutoplayFailed, Message: "The browser blocked automatic playback. Start playback from Smolify."})
	})
}

func (p *Player) listen(event string, callback func([]js.Value)) {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		callback(args)
		return nil
	})
	p.player.Call("addListener", event, fn)
	p.listeners = append(p.listeners, listener{event: event, fn: fn})
}

func stateFromJS(value js.Value) webspotify.SDKState {
	trackWindow := property(value, "track_window")
	current := property(trackWindow, "current_track")
	if current.IsUndefined() || current.IsNull() {
		return webspotify.SDKState{Empty: true}
	}
	track := model.Track{
		URI:      propertyString(current, "uri"),
		Name:     propertyString(current, "name"),
		Duration: time.Duration(propertyFloat(current, "duration_ms")) * time.Millisecond,
	}
	if track.URI == "" {
		if id := propertyString(current, "id"); id != "" {
			track.URI = "spotify:track:" + id
		}
	}
	artists := property(current, "artists")
	for i := 0; !artists.IsUndefined() && !artists.IsNull() && i < artists.Length(); i++ {
		artist := artists.Index(i)
		track.Artists = append(track.Artists, propertyString(artist, "name"))
		track.ArtistURIs = append(track.ArtistURIs, propertyString(artist, "uri"))
	}
	album := property(current, "album")
	track.Album = propertyString(album, "name")
	track.AlbumURI = propertyString(album, "uri")
	images := property(album, "images")
	if !images.IsUndefined() && !images.IsNull() && images.Length() > 0 {
		track.Cover = propertyString(images.Index(0), "url")
	}
	context := property(value, "context")
	contextURI := propertyString(context, "uri")
	return webspotify.SDKState{
		Track: track, ContextURI: contextURI,
		Position: time.Duration(propertyFloat(value, "position")) * time.Millisecond,
		Duration: track.Duration, Paused: propertyBool(value, "paused"),
	}
}

func property(value js.Value, key string) js.Value {
	if value.IsUndefined() || value.IsNull() || value.Type() != js.TypeObject {
		return js.Undefined()
	}
	return value.Get(key)
}

func propertyString(value js.Value, key string) string {
	got := property(value, key)
	if got.IsUndefined() || got.IsNull() {
		return ""
	}
	return got.String()
}

func propertyFloat(value js.Value, key string) float64 {
	got := property(value, key)
	if got.IsUndefined() || got.IsNull() {
		return 0
	}
	return got.Float()
}

func propertyBool(value js.Value, key string) bool {
	got := property(value, key)
	return !got.IsUndefined() && !got.IsNull() && got.Bool()
}

func (p *Player) emit(event webspotify.SDKEvent) {
	select {
	case p.events <- event:
	default:
	}
}

func (p *Player) ActivateElement() error {
	if p.player.IsUndefined() || p.player.IsNull() {
		return nil
	}
	if activate := p.player.Get("activateElement"); activate.Type() == js.TypeFunction {
		p.player.Call("activateElement")
	}
	return nil
}

func (p *Player) SetName(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("browser playback device name cannot be empty")
	}
	p.mu.Lock()
	if p.name == name {
		p.mu.Unlock()
		return nil
	}
	connected := p.connected
	p.mu.Unlock()
	if connected {
		if err := p.Disconnect(); err != nil {
			return err
		}
	}
	p.name = name
	if connected {
		return p.Connect(ctx)
	}
	return nil
}

func (p *Player) Disconnect() error {
	p.mu.Lock()
	player := p.player
	listeners := p.listeners
	tokenFn := p.tokenFn
	p.listeners = nil
	p.connected = false
	p.player = js.Undefined()
	p.mu.Unlock()
	if player.IsUndefined() || player.IsNull() {
		return nil
	}
	for _, listener := range listeners {
		player.Call("removeListener", listener.event, listener.fn)
	}
	player.Call("disconnect")
	for _, listener := range listeners {
		listener.fn.Release()
	}
	tokenFn.Release()
	return nil
}

func waitBoolean(ctx context.Context, promise js.Value) (bool, error) {
	type result struct {
		value bool
		err   error
	}
	settled := make(chan result, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := len(args) > 0 && args[0].Bool()
		settled <- result{value: value}
		return nil
	})
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "Spotify Web Playback SDK connect failed"
		if len(args) > 0 {
			message = fmt.Sprint(args[0])
		}
		settled <- result{err: errors.New(message)}
		return nil
	})
	promise.Call("then", resolve, reject)
	select {
	case got := <-settled:
		resolve.Release()
		reject.Release()
		return got.value, got.err
	case <-ctx.Done():
		go func() {
			<-settled
			resolve.Release()
			reject.Release()
		}()
		return false, ctx.Err()
	}
}
