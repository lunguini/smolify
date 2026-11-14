package app

import (
	"image"
	"image/color"
)

// shrink is im scaled down to fit px×px, each pixel the average of the
// ones it covers (im itself when it fits).
func shrink(im image.Image, px int) image.Image {
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= px && h <= px || w == 0 || h == 0 {
		return im
	}
	scale := float64(px) / float64(max(w, h))
	dw, dh := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0, y1 := b.Min.Y+y*h/dh, b.Min.Y+max((y+1)*h/dh, y*h/dh+1)
		for x := range dw {
			x0, x1 := b.Min.X+x*w/dw, b.Min.X+max((x+1)*w/dw, x*w/dw+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := im.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return out
}
