//go:build !js || !wasm

package app

import (
	"context"
	"os/exec"

	"github.com/lunguini/flatte"
)

// openURL opens url in the user's default browser.
func openURL(s *State, url string) {
	if url == "" {
		return
	}
	flatte.Go(s.fx, "open-url", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, exec.CommandContext(ctx, "open", url).Run()
	}, func(s *State, _ struct{}, err error) {
		if err != nil {
			fail(s, "Could not open the browser: "+err.Error())
		}
	})
}
