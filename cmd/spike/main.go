// Command spike checks that Smolify builds against Flatte and go-librespot.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/window"
)

type state struct{}

func main() {
	gui := flag.Bool("gui", false, "run in a window")
	flag.Parse()
	app := flatte.App[state]{
		State: &state{},
		Handle: func(_ *state, ev flatte.Event, fx flatte.Effects[state]) {
			if k, ok := ev.(flatte.KeyEvent); ok && (k.Key == flatte.KeyEscape || k.Rune == 'q') {
				fx.Quit()
			}
		},
		View: func(*state, flatte.RenderContext) flatte.Frame {
			return flatte.Frame{Title: "Smolify", Root: layout.Label{Text: "Smolify spike — q quits"}}
		},
	}
	var err error
	if *gui {
		err = window.Run(context.Background(), app, window.Options{Title: "Smolify", Width: 40, Height: 6})
	} else {
		err = flatte.Run(context.Background(), app)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
