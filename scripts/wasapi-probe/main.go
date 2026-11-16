// Command wasapi-probe plays a short tone through go-librespot's WASAPI
// output, the backend Smolify picks on Windows. It needs no Spotify account
// and builds without cgo, so it can be cross-compiled and run on Windows or
// under Wine:
//
//	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/wasapi-probe.exe ./scripts/wasapi-probe
//	wine dist/wasapi-probe.exe
//
// A tone of about two seconds and exit status 0 mean the output works.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sync/atomic"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/output"
)

const sampleRate = 48000

// tone is a stereo sine wave.
type tone struct {
	freq  float64
	n     int
	reads atomic.Int64
}

func (t *tone) Read(samples []float32) (int, error) {
	for i := 0; i+1 < len(samples); i += 2 {
		v := float32(0.2 * math.Sin(2*math.Pi*t.freq*float64(t.n)/sampleRate))
		samples[i], samples[i+1] = v, v
		t.n++
	}
	t.reads.Add(1)
	return len(samples) &^ 1, nil
}

func main() {
	backend := flag.String("backend", "wasapi", "go-librespot output backend")
	secs := flag.Duration("for", 2*time.Second, "how long to play")
	freq := flag.Float64("freq", 440, "tone frequency in Hz")
	flag.Parse()
	if err := run(*backend, *secs, *freq); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run(backend string, secs time.Duration, freq float64) error {
	fmt.Printf("opening %q output, %d Hz stereo\n", backend, sampleRate)
	src := &tone{freq: freq}
	out, err := output.NewOutput(&output.NewOutputOptions{
		Backend:       backend,
		Device:        "default", // what Smolify passes; WASAPI ignores it
		Reader:        librespot.Float32Reader(src),
		SampleRate:    sampleRate,
		ChannelCount:  2,
		InitialVolume: 1,
		VolumeUpdate:  make(chan float32, 8),
	})
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	if err := out.Resume(); err != nil {
		_ = out.Close()
		return fmt.Errorf("start output: %w", err)
	}
	fmt.Printf("playing a %.0f Hz tone for %s...\n", freq, secs)
	select {
	case err := <-out.Error():
		_ = out.Close()
		if err == nil {
			err = errors.New("output stopped early")
		}
		return fmt.Errorf("output: %w", err)
	case <-time.After(secs):
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	reads := src.reads.Load()
	if reads == 0 {
		return errors.New("the output never asked for audio")
	}
	fmt.Printf("OK: the device pulled audio %d times (%.1fs of samples)\n", reads, float64(src.n)/sampleRate)
	return nil
}
