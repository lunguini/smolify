// Command smolify is a small, fast Spotify client: in the terminal, or in a
// native window with -gui. It plays through the Mac's audio output (Spotify
// Premium required).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/app"
	"github.com/lunguini/smolify/internal/librarycache"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

func main() {
	gui := flag.Bool("gui", false, "run in a native window instead of the terminal")
	signOut := flag.Bool("sign-out", false, "forget the stored Spotify sign-in and exit")
	debugLog := flag.String("debug-log", "", "log player events, commands, requests, and memory to this file")
	heapProfile := flag.String("heap-profile", "", "write a heap profile to this file on exit")
	flag.Parse()

	dir, err := os.UserConfigDir()
	if err != nil {
		fail(err)
	}
	dir = filepath.Join(dir, "smolify")
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		fail(err)
	}
	cacheDir = filepath.Join(cacheDir, "smolify", "library")
	if *signOut {
		if err := librarycache.New(cacheDir).PurgeAll(); err != nil {
			fail(err)
		}
		if err := player.SignOut(dir); err != nil {
			fail(err)
		}
		fmt.Println("Signed out.")
		return
	}

	ctx := context.Background()
	settingsPath := app.SettingsPath(dir)
	settings, err := app.LoadSettings(settingsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smolify: settings:", err, "(using the defaults)")
	}
	p, err := player.Start(ctx, player.Options{Dir: dir, DeviceName: settings.DeviceName, Audio: settings.Audio()})
	if err != nil {
		fail(err)
	}
	defer p.Close()

	var debug io.Writer
	if *debugLog != "" {
		f, err := os.OpenFile(*debugLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		debug = f
		go logMemory(ctx, f)
	}
	if *heapProfile != "" {
		defer writeHeapProfile(*heapProfile)
	}
	web := &spotify.Client{Token: p.Token, ClientToken: p.ClientToken}
	a := app.WithCache(app.WithSettings(app.New(p, app.HTTPImages{}, web, debug), settingsPath, settings), cacheDir)
	if *gui {
		a = app.Windowed(a)
		theme := app.WindowTheme()
		err = window.Run(ctx, a, window.Options{Title: "Smolify", Theme: &theme, Fonts: app.WindowFonts(), HideTitleBar: true, Translucent: true, Width: 110, Height: 34, MinWidth: 70, MinHeight: 20, Autosave: "smolify.main"})
	} else {
		err = window.RunTerminal(ctx, a, flatte.WithMouse(flatte.MouseModeCellMotion), flatte.WithoutDefaultQuit(), flatte.WithReportFocus())
	}
	if err != nil {
		fail(err)
	}
}

// logMemory writes the Go runtime's memory figures to w every 10 seconds:
// what Go holds, against the process's footprint in Activity Monitor (the
// rest is the audio stack, system frameworks, and other native memory).
func logMemory(ctx context.Context, w io.Writer) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		mib := func(n uint64) float64 { return float64(n) / (1 << 20) }
		fmt.Fprintf(w, "%s memory: heap live %.1f MiB, heap in use %.1f MiB, heap from OS %.1f MiB, Go total %.1f MiB, stacks %.1f MiB, GCs %d\n",
			time.Now().Format("15:04:05.000"), mib(m.HeapAlloc), mib(m.HeapInuse), mib(m.HeapSys-m.HeapReleased), mib(m.Sys), mib(m.StackSys), m.NumGC)
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}
}

func writeHeapProfile(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smolify:", err)
		return
	}
	defer f.Close()
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		fmt.Fprintln(os.Stderr, "smolify:", err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "smolify:", err)
	os.Exit(1)
}
