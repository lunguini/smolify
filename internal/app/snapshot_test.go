package app

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/player"
)

// TestWindowSnapshot renders the main window offscreen, for looking at the
// design: SMOLIFY_SNAPSHOT=<dir> go test -run TestWindowSnapshot ./internal/app
func TestWindowSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := New(&fake{}, nil, nil)
	s := a.State
	s.ready = true
	s.playlists = []player.Playlist{
		{URI: "p:1", Name: "OverThink Radio", Owner: "Spotify", Length: 50, Image: "c:1", Description: "With Kuo, Lin and more. Updated daily."},
		{URI: "p:2", Name: "bass house", Owner: "adrian", Length: 42, Image: "c:2"},
		{URI: "p:3", Name: "Podcasts", Owner: "adrian", Length: 12},
		{URI: "p:4", Name: "Insane — Alastor (Hazbin Hotel)", Owner: "adrian", Length: 3},
		{URI: "p:5", Name: "Your Top Songs 2023", Owner: "Spotify", Length: 101},
		{URI: "p:6", Name: "Chill Bath Mix", Owner: "Spotify", Length: 50},
		{URI: "p:7", Name: "Car Music", Owner: "adrian", Length: 7},
	}
	s.rail.SetCount(len(s.playlists))
	s.rail.SetHeight(20)
	s.covers["c:1"] = art(color.RGBA{230, 80, 120, 255}, color.RGBA{60, 40, 160, 255})
	s.covers["c:2"] = art(color.RGBA{30, 200, 140, 255}, color.RGBA{20, 40, 60, 255})
	s.covers["c:t"] = art(color.RGBA{250, 180, 60, 255}, color.RGBA{120, 30, 60, 255})
	s.track = &player.Track{URI: "t:1", Name: "OverThink", Artists: []string{"Kuo", "Lin"}, Duration: 213 * time.Second, Cover: "c:t", ContextURI: "p:1"}
	s.context, s.playing, s.position, s.volume = "p:1", true, 74*time.Second, 60
	img, err := window.Snapshot(a, window.Options{Width: 110, Height: 34}, 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "smolify-main.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// art is a 300×300 diagonal gradient with a soft disc, standing in for
// cover art.
func art(a, b color.RGBA) image.Image {
	const n = 300
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			t := float64(x+y) / (2 * n)
			mix := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*t) }
			c := color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
			if math.Hypot(float64(x)-n*0.62, float64(y)-n*0.4) < n*0.22 {
				c = color.RGBA{255, 240, 220, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
