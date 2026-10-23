package app

import (
	"image"
	"image/color"
	"math"
	"sync"
)

// smolArt is Smolify's own cover, for playlists without one (and Liked
// Songs): deep indigo lit by a violet glow and a magenta one, with faint
// sound-wave rings rising from the bottom. Drawn once, artPixels square.
var smolArt = sync.OnceValue(func() image.Image {
	const n = artPixels
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	type glow struct {
		x, y, r, strength float64
		c                 [3]float64
	}
	glows := []glow{
		{0.22, 0.18, 0.75, 0.85, [3]float64{0x9d, 0x7b, 0xff}}, // the brand violet
		{0.9, 0.92, 0.65, 0.5, [3]float64{0xe0, 0x5a, 0xc8}},
	}
	for y := range n {
		for x := range n {
			u, v := (float64(x)+0.5)/n, (float64(y)+0.5)/n
			c := [3]float64{0x24 - 0x10*v, 0x16 - 0x0a*v, 0x52 - 0x24*v} // indigo, darker downward
			for _, g := range glows {
				d := math.Hypot(u-g.x, v-g.y) / g.r
				if d >= 1 {
					continue
				}
				f := g.strength * (1 - d) * (1 - d)
				for i := range 3 {
					c[i] += (g.c[i] - c[i]) * f
				}
			}
			// Rings around a point below the bottom edge, fading upward.
			d := math.Hypot(u-0.5, v-1.15)
			ring := math.Pow(math.Max(math.Cos(d*2*math.Pi*7), 0), 6) * 0.1 * math.Max(1-d, 0)
			for i := range 3 {
				c[i] += (0xff - c[i]) * ring
			}
			out.SetRGBA(x, y, color.RGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), 0xff})
		}
	}
	return out
})

// blankArt is a cover still loading: one pixel of the tile color.
var blankArt = func() image.Image {
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	r, g, b, _ := tileBg.RGBA()
	im.SetRGBA(0, 0, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xff})
	return im
}()

// artPixels is smolArt's size: as large as a fetched header cover.
const artPixels = coverPixels
