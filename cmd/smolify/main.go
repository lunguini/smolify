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

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/app"
	"github.com/lunguini/smolify/internal/player"
)

func main() {
	gui := flag.Bool("gui", false, "run in a native window instead of the terminal")
	signOut := flag.Bool("sign-out", false, "forget the stored Spotify sign-in and exit")
	flag.Parse()

	dir, err := os.UserConfigDir()
	if err != nil {
		fail(err)
	}
	dir = filepath.Join(dir, "smolify")
	if *signOut {
		if err := player.SignOut(dir); err != nil {
			fail(err)
		}
		fmt.Println("Signed out.")
		return
	}

	ctx := context.Background()
	p, err := player.Start(ctx, player.Options{Dir: dir, DeviceName: "Smolify"})
	if err != nil {
		fail(err)
	}
	defer p.Close()

	// SMOLIFY_DEBUG=<file> logs what the UI receives and sends.
	var debug io.Writer
	if path := os.Getenv("SMOLIFY_DEBUG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		debug = f
	}
	a := app.New(p, app.HTTPImages{}, debug)
	if *gui {
		err = window.Run(ctx, a, window.Options{Title: "Smolify", Width: 110, Height: 34, MinWidth: 70, MinHeight: 20, Autosave: "smolify.main"})
	} else {
		err = flatte.Run(ctx, a, flatte.WithMouse(flatte.MouseModeCellMotion), flatte.WithoutDefaultQuit())
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "smolify:", err)
	os.Exit(1)
}
