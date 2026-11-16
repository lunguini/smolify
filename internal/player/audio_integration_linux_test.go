//go:build linux && audiointegration

package player

import (
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/output"

	"github.com/lunguini/smolify/internal/model"
)

type silenceAudioReader struct{ reads atomic.Int32 }

func (r *silenceAudioReader) Read(samples []float32) (int, error) {
	clear(samples)
	r.reads.Add(1)
	return len(samples), nil
}

// TestPulseAudioSilenceStream opens and feeds a silent stream to the default
// PulseAudio sink. Run with -tags audiointegration and a Pulse-compatible
// server on PULSE_SERVER; it needs no Spotify account or credentials.
func TestPulseAudioSilenceStream(t *testing.T) {
	if os.Getenv("SMOLIFY_AUDIO_INTEGRATION") != "1" {
		t.Skip("set SMOLIFY_AUDIO_INTEGRATION=1 to open a PulseAudio stream")
	}
	backend, err := model.ResolveAudioBackend(runtime.GOOS, model.AudioBackendAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	if backend != "pulseaudio" {
		t.Fatalf("Linux automatic backend %q, want pulseaudio", backend)
	}
	reader := &silenceAudioReader{}
	volumeUpdates := make(chan float32, 4)
	out, err := output.NewOutput(&output.NewOutputOptions{
		Backend:       backend,
		RuntimeSocket: os.Getenv("PULSE_SERVER"),
		Reader:        librespot.Float32Reader(reader),
		SampleRate:    48000,
		ChannelCount:  2,
		VolumeUpdate:  volumeUpdates,
	})
	if err != nil {
		t.Fatalf("open PulseAudio output: %v", err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			t.Errorf("close PulseAudio output: %v", err)
		}
	}()
	if err := out.Resume(); err != nil {
		t.Fatalf("start PulseAudio output: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for reader.reads.Load() == 0 && time.Now().Before(deadline) {
		select {
		case err := <-out.Error():
			if err != nil {
				t.Fatalf("PulseAudio output: %v", err)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if reader.reads.Load() == 0 {
		t.Fatal("PulseAudio opened but did not consume any silent samples")
	}
	select {
	case err := <-out.Error():
		if err != nil {
			t.Fatalf("PulseAudio output: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
	}
	if delay, err := out.DelayMs(); err != nil || delay < 0 {
		t.Fatalf("PulseAudio delay %d ms, %v", delay, err)
	}
}
