package model

import (
	"fmt"
	"strings"
)

// AudioBackendAuto selects Smolify's default output driver for the OS.
const AudioBackendAuto = "auto"

// AudioBackendEnv is the environment variable that overrides the saved
// output driver.
const AudioBackendEnv = "SMOLIFY_AUDIO_BACKEND"

// AudioBackendOptions are the output drivers Smolify can use on goos, with
// AudioBackendAuto first; none in a browser, which plays through its own
// player. go-librespot's pipe driver is left out: it needs a FIFO path.
func AudioBackendOptions(goos string) []string {
	backends := audioBackends(goos)
	if len(backends) == 0 {
		return nil
	}
	return append([]string{AudioBackendAuto}, backends...)
}

// KnownAudioBackend reports whether backend is one of goos's options.
func KnownAudioBackend(goos, backend string) bool {
	for _, option := range AudioBackendOptions(goos) {
		if option == backend {
			return true
		}
	}
	return false
}

// ResolveAudioBackend is the output driver to use on goos: the override
// (the environment) when set, else the saved setting, with "" and
// AudioBackendAuto meaning the OS default.
func ResolveAudioBackend(goos, setting, override string) (string, error) {
	if override = strings.TrimSpace(override); override != "" {
		setting = override
	}
	setting = strings.TrimSpace(setting)
	backends := audioBackends(goos)
	if len(backends) == 0 {
		return "", fmt.Errorf("no audio output to choose on %s", goos)
	}
	if setting == "" || setting == AudioBackendAuto {
		return backends[0], nil
	}
	for _, backend := range backends {
		if setting == backend {
			return backend, nil
		}
	}
	return "", fmt.Errorf("audio output %q is unavailable on %s (available: %s)", setting, goos, strings.Join(backends, ", "))
}

// audioBackends are goos's output drivers, its default first.
func audioBackends(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"audio-toolbox", "pulseaudio"}
	case "windows":
		return []string{"wasapi", "pulseaudio"}
	case "linux":
		return []string{"pulseaudio", "alsa"}
	case "js":
		return nil
	default:
		return []string{"alsa", "pulseaudio"}
	}
}
