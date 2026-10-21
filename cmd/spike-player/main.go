// Command spike-player embeds go-librespot without a UI: it signs in with
// Spotify's device pairing, lists the library's playlists, plays the first
// one, prints the player's events, and pauses after a while. It proves the
// in-process player before Smolify's UI is built on it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
)

func main() {
	listen := flag.Duration("listen", 20*time.Second, "how long to play before pausing")
	verbose := flag.Bool("v", false, "log the player's debug output")
	flag.Parse()

	dir, err := os.UserConfigDir()
	if err != nil {
		fail(err)
	}
	dir = filepath.Join(dir, "smolify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(err)
	}

	server := &inProcess{requests: make(chan daemon.ApiRequest), events: make(chan *daemon.ApiEvent, 64)}
	app, err := daemon.New(&daemon.Options{
		Logger:     logger{verbose: *verbose},
		StateStore: fileStore{path: filepath.Join(dir, "state.json")},
		APIServer:  server,
		Config: &daemon.Config{
			DeviceName:    "Smolify (spike)",
			DeviceType:    "computer",
			AudioBackend:  "audio-toolbox",
			Bitrate:       160,
			VolumeSteps:   100,
			InitialVolume: 40,
			SkipDebounce:  600 * time.Millisecond,
			ImageSize:     "default",
			Metadata:      daemon.MetadataConfig{Enabled: true, MaxTracks: 800},
			Credentials:   daemon.CredentialsConfig{Type: "device_auth"},
		},
	})
	if err != nil {
		fail(err)
	}
	defer app.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "player stopped:", err)
			stop()
		}
	}()
	go func() {
		for ev := range server.events {
			b, _ := json.Marshal(ev.Data)
			if len(b) > 160 {
				b = append(b[:160], "…"...)
			}
			fmt.Printf("event %-14s %s\n", ev.Type, b)
		}
	}()

	// Requests fail until the session is up (after pairing): retry.
	var playlists daemon.ApiLibraryPlaylists
	for {
		data, err := request(ctx, server, daemon.ApiRequestTypeLibraryPlaylists, daemon.ApiRequestDataLibraryPlaylists{Limit: 10})
		if err == nil {
			playlists = *data.(*daemon.ApiLibraryPlaylists)
			break
		}
		if ctx.Err() != nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Printf("\n%d playlists in the library; first %d:\n", playlists.Total, len(playlists.Items))
	for _, p := range playlists.Items {
		fmt.Printf("  %-40s %s\n", p.Name, p.Uri)
	}
	if len(playlists.Items) == 0 {
		return
	}
	first := playlists.Items[0]
	fmt.Printf("\nplaying %q for %s…\n", first.Name, *listen)
	if _, err := request(ctx, server, daemon.ApiRequestTypePlay, daemon.ApiPlay{Uri: first.Uri}); err != nil {
		fail(err)
	}
	select {
	case <-time.After(*listen):
	case <-ctx.Done():
	}
	if _, err := request(context.Background(), server, daemon.ApiRequestTypePause, nil); err != nil {
		fmt.Fprintln(os.Stderr, "pause:", err)
	}
	time.Sleep(time.Second) // let the pause event print
}

// request sends one request to the player and waits for its reply.
func request(ctx context.Context, s *inProcess, t daemon.ApiRequestType, data any) (any, error) {
	req, wait := daemon.NewApiRequest(t, data)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case s.requests <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return wait(ctx)
}

// inProcess is an ApiServer that hands requests to the player over a
// channel and collects its events — no HTTP. The pairing code is printed.
type inProcess struct {
	requests chan daemon.ApiRequest
	events   chan *daemon.ApiEvent
}

func (s *inProcess) Emit(ev *daemon.ApiEvent) {
	select {
	case s.events <- ev:
	default: // never block the player on a slow reader
	}
}

func (s *inProcess) Receive() <-chan daemon.ApiRequest { return s.requests }

func (s *inProcess) SetAuthCode(auth *daemon.ApiDeviceAuth) {
	if auth == nil {
		fmt.Println("paired.")
		return
	}
	fmt.Printf("\nSign in: open %s\n(or go to spotify.com/pair and enter %s) — expires %s\n\n",
		auth.Url, auth.Code, auth.ExpiresAt.Local().Format(time.Kitchen))
}

func (s *inProcess) Close() error { return nil }

// fileStore keeps the player's state (device ID, stored credentials) in a
// JSON file.
type fileStore struct{ path string }

func (f fileStore) Load() (*librespot.AppState, error) {
	st := &librespot.AppState{}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	return st, json.Unmarshal(b, st)
}

func (f fileStore) Save(st *librespot.AppState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, b, 0o600)
}

// logger prints the player's warnings and errors (and info/debug with -v).
type logger struct {
	verbose bool
	fields  string
}

func (l logger) out(level string, quiet bool, msg string) {
	if quiet && !l.verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s%s\n", level, msg, l.fields)
}

func (l logger) Tracef(f string, a ...any) {}
func (l logger) Debugf(f string, a ...any) { l.out("debug", true, fmt.Sprintf(f, a...)) }
func (l logger) Infof(f string, a ...any)  { l.out("info ", true, fmt.Sprintf(f, a...)) }
func (l logger) Warnf(f string, a ...any)  { l.out("warn ", false, fmt.Sprintf(f, a...)) }
func (l logger) Errorf(f string, a ...any) { l.out("error", false, fmt.Sprintf(f, a...)) }
func (l logger) Trace(a ...any)            {}
func (l logger) Debug(a ...any)            { l.out("debug", true, fmt.Sprint(a...)) }
func (l logger) Info(a ...any)             { l.out("info ", true, fmt.Sprint(a...)) }
func (l logger) Warn(a ...any)             { l.out("warn ", false, fmt.Sprint(a...)) }
func (l logger) Error(a ...any)            { l.out("error", false, fmt.Sprint(a...)) }
func (l logger) WithField(k string, v any) librespot.Logger {
	l.fields += fmt.Sprintf(" %s=%v", k, v)
	return l
}
func (l logger) WithError(err error) librespot.Logger { return l.WithField("error", err) }

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
