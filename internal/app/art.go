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

// blankArt is a cover still loading: one pixel of the look's tile color,
// the same image for the same color so hosts keep their copy of it.
func blankArt(tile color.NRGBA) image.Image {
	tile.A = 0xff
	if im, ok := blankArts.Load(tile); ok {
		return im.(image.Image)
	}
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	im.SetRGBA(0, 0, color.RGBA{tile.R, tile.G, tile.B, 0xff})
	got, _ := blankArts.LoadOrStore(tile, image.Image(im))
	return got.(image.Image)
}

var blankArts sync.Map // color.NRGBA → image.Image

// artPixels is smolArt's size: as large as a fetched header cover.
const artPixels = coverPixels

// heartMiddle is the heart curve's vertical middle, between its tip (-1)
// and the top of its lobes (about 1.236).
const heartMiddle = 0.118

// likedArt is Liked Songs' cover as one picture, for terminals: a white
// heart on smolArt, about half its size, edges antialiased.
var likedArt = sync.OnceValue(func() image.Image {
	const n, samples = artPixels, 4
	base := smolArt()
	out := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			inside := 0
			for sy := range samples {
				for sx := range samples {
					// The heart curve (x²+y²-1)³ - x²y³ ≤ 0, scaled to about
					// the middle half. It spans y from -1 (its tip) to about
					// 1.236 (its lobes), so its middle, y≈0.118, is put at
					// the picture's center.
					u := ((float64(x)+(float64(sx)+0.5)/samples)/n - 0.5) * 4.2
					v := (0.5 + heartMiddle/4.2 - (float64(y)+(float64(sy)+0.5)/samples)/n) * 4.2
					if a := u*u + v*v - 1; a*a*a-u*u*v*v*v <= 0 {
						inside++
					}
				}
			}
			r, g, b, _ := base.At(x, y).RGBA()
			f := float64(inside) / samples / samples
			mix := func(c uint32) uint8 { return uint8(float64(c>>8) + (0xff-float64(c>>8))*f) }
			out.SetRGBA(x, y, color.RGBA{mix(r), mix(g), mix(b), 0xff})
		}
	}
	return out
})
