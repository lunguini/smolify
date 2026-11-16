//go:build !js || !wasm

package player

import (
	"context"
	"runtime"
	"testing"

	"github.com/devgianlu/go-librespot/daemon"

	"github.com/lunguini/smolify/internal/model"
)

type sessionAppFunc func(context.Context) error

func (f sessionAppFunc) Run(ctx context.Context) error { return f(ctx) }

// launch hands go-librespot the output driver the setting and the
// environment resolve to on this OS, and its default device.
func TestLaunchPassesResolvedAudioBackend(t *testing.T) {
	defaultBackend, err := model.ResolveAudioBackend(runtime.GOOS, model.AudioBackendAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	options := model.AudioBackendOptions(runtime.GOOS)
	other := options[len(options)-1] // a non-default choice on every OS
	for _, tt := range []struct {
		name, setting, override, want string
	}{
		{name: "automatic", setting: model.AudioBackendAuto, want: defaultBackend},
		{name: "unset", setting: "", want: defaultBackend},
		{name: "saved choice", setting: other, want: other},
		{name: "environment override", setting: defaultBackend, override: other, want: other},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(model.AudioBackendEnv, tt.override)
			got := make(chan *daemon.Config, 1)
			p := &Player{
				events: make(chan Event, 1),
				ctx:    context.Background(),
				opts:   Options{Dir: t.TempDir(), Audio: Audio{Backend: tt.setting}},
				newApp: func(o *daemon.Options) (sessionApp, error) {
					got <- o.Config
					return sessionAppFunc(func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }), nil
				},
			}
			p.mu.Lock()
			launchErr := p.launch()
			p.mu.Unlock()
			if launchErr != nil {
				t.Fatal(launchErr)
			}
			t.Cleanup(p.Close)
			c := <-got
			if c.AudioBackend != tt.want || c.AudioDevice != "default" {
				t.Fatalf("daemon output %q device %q, want %q device default", c.AudioBackend, c.AudioDevice, tt.want)
			}
		})
	}
}

// An output this OS lacks fails the launch, naming it, instead of failing
// every track later.
func TestLaunchRejectsAnUnavailableAudioBackend(t *testing.T) {
	t.Setenv(model.AudioBackendEnv, "no-such-output")
	p := &Player{events: make(chan Event, 1), ctx: context.Background(), opts: Options{Dir: t.TempDir()},
		newApp: func(*daemon.Options) (sessionApp, error) {
			t.Fatal("launched with an unavailable output")
			return nil, nil
		}}
	p.mu.Lock()
	err := p.launch()
	p.mu.Unlock()
	if err == nil {
		t.Fatal("launch accepted an unavailable output")
	}
}
