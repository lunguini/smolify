package app

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/player"
)

func TestShrink(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 640, 320))
	for y := range 320 {
		for x := range 640 {
			c := color.RGBA{0, 0, 0, 255}
			if x%2 == 0 {
				c = color.RGBA{200, 100, 50, 255}
			}
			im.SetRGBA(x, y, c)
		}
	}
	out := shrink(im, 96)
	if b := out.Bounds(); b.Dx() != 96 || b.Dy() != 48 {
		t.Fatalf("shrunk to %v, want 96×48", b)
	}
	// Each pixel averages the 6 or 7 stripes it covers: about half colored.
	if got := out.At(10, 10).(color.RGBA); got.R < 85 || got.R > 118 || got.G < 42 || got.G > 59 {
		t.Fatalf("average %v, want about {100 50 25}", got)
	}
	small := image.NewRGBA(image.Rect(0, 0, 40, 40))
	if shrink(small, 96) != image.Image(small) {
		t.Fatal("a cover that fits was copied")
	}
}

func TestTotal(t *testing.T) {
	min := func(n int) player.Track { return player.Track{Duration: time.Duration(n) * time.Minute} }
	if got := total([]player.Track{min(30), min(22)}); got != "52 min" {
		t.Fatal(got)
	}
	if got := total([]player.Track{min(120), min(72), min(1)}); got != "3 h 13 min" {
		t.Fatal(got)
	}
}
