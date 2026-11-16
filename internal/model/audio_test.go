package model

import (
	"slices"
	"testing"
)

func TestAudioBackendSelection(t *testing.T) {
	tests := []struct {
		goos           string
		options        []string
		defaultBackend string
	}{
		{"darwin", []string{"auto", "audio-toolbox", "pulseaudio"}, "audio-toolbox"},
		{"linux", []string{"auto", "pulseaudio", "alsa"}, "pulseaudio"},
		{"windows", []string{"auto", "wasapi", "pulseaudio"}, "wasapi"},
		{"freebsd", []string{"auto", "alsa", "pulseaudio"}, "alsa"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := AudioBackendOptions(tt.goos); !slices.Equal(got, tt.options) {
				t.Fatalf("options %v, want %v", got, tt.options)
			}
			for _, setting := range []string{"auto", "", "  auto "} {
				got, err := ResolveAudioBackend(tt.goos, setting, "")
				if err != nil || got != tt.defaultBackend {
					t.Fatalf("automatic backend for %q: %q, %v; want %q", setting, got, err, tt.defaultBackend)
				}
			}
		})
	}

	if got, err := ResolveAudioBackend("linux", "alsa", "pulseaudio"); err != nil || got != "pulseaudio" {
		t.Fatalf("environment override %q, %v; want pulseaudio", got, err)
	}
	if got, err := ResolveAudioBackend("linux", "alsa", "  "); err != nil || got != "alsa" {
		t.Fatalf("blank override %q, %v; want the setting", got, err)
	}
	if _, err := ResolveAudioBackend("linux", "audio-toolbox", ""); err == nil {
		t.Fatal("accepted a backend unavailable on Linux")
	}
	if _, err := ResolveAudioBackend("linux", "auto", "wasapi"); err == nil {
		t.Fatal("accepted an environment backend unavailable on Linux")
	}
}

// A browser plays through its own player: there is no output to choose.
func TestAudioBackendOptionsInABrowser(t *testing.T) {
	if got := AudioBackendOptions("js"); len(got) != 0 {
		t.Fatalf("browser options %v, want none", got)
	}
}

func TestKnownAudioBackend(t *testing.T) {
	for _, tt := range []struct {
		goos, backend string
		ok            bool
	}{
		{"linux", "alsa", true},
		{"linux", "auto", true},
		{"linux", "wasapi", false},
		{"darwin", "", false},
		{"js", "auto", false},
	} {
		if got := KnownAudioBackend(tt.goos, tt.backend); got != tt.ok {
			t.Errorf("KnownAudioBackend(%q, %q) = %v, want %v", tt.goos, tt.backend, got, tt.ok)
		}
	}
}
