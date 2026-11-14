package demo

import (
	"context"
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

func TestDemoPlaybackAdvancesAndMovesTracks(t *testing.T) {
	b := New()
	<-b.Events() // Ready
	ctx := context.Background()
	if err := b.Play(ctx, "spotify:playlist:demo-night", ""); err != nil {
		t.Fatal(err)
	}
	status, err := b.Status(ctx)
	if err != nil || status.Track == nil || status.Track.URI != "spotify:track:demo-01" || status.Paused {
		t.Fatalf("initial status = %+v, err=%v", status, err)
	}
	b.Advance(11 * time.Second)
	status, _ = b.Status(ctx)
	if status.Track.Position != 11*time.Second {
		t.Fatalf("position = %v, want 11s", status.Track.Position)
	}
	if err := b.Next(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ = b.Status(ctx)
	if status.Track.URI != "spotify:track:demo-02" || status.Track.Position != 0 {
		t.Fatalf("after next = %+v", status.Track)
	}
}

func TestDemoShuffleRepeatVolumeAndLikes(t *testing.T) {
	b := New()
	<-b.Events()
	ctx := context.Background()
	if err := b.Play(ctx, "spotify:playlist:demo-night", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.SetShuffle(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := b.Next(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ := b.Status(ctx)
	if !status.Shuffle || status.Track.URI != "spotify:track:demo-07" {
		t.Fatalf("shuffle status = %+v", status)
	}
	if err := b.SetRepeat(ctx, model.Repeat{Track: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.Next(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ = b.Status(ctx)
	if !status.Repeat.Track || status.Track.URI != "spotify:track:demo-07" {
		t.Fatalf("repeat status = %+v", status)
	}
	if err := b.SetVolume(ctx, 73); err != nil {
		t.Fatal(err)
	}
	likedURI := "spotify:track:demo-01"
	if err := b.SetLiked(ctx, []string{likedURI}, true); err != nil {
		t.Fatal(err)
	}
	states, err := b.Liked(ctx, []string{likedURI, "spotify:track:demo-03"})
	if err != nil || len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("liked states = %v, err=%v", states, err)
	}
	status, _ = b.Status(ctx)
	if status.Volume != 73 {
		t.Fatalf("volume = %d", status.Volume)
	}
}

func TestDemoQueueSearchAndArtwork(t *testing.T) {
	b := New()
	<-b.Events()
	ctx := context.Background()
	if err := b.Play(ctx, "spotify:playlist:demo-night", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Queue(ctx, "spotify:track:demo-05"); err != nil {
		t.Fatal(err)
	}
	if err := b.Next(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ := b.Status(ctx)
	if status.Track.URI != "spotify:track:demo-05" {
		t.Fatalf("queued track = %+v", status.Track)
	}
	results, err := b.Search(ctx, "paper", 10)
	if err != nil || len(results.Tracks) == 0 || len(results.Albums) == 0 {
		t.Fatalf("search results = %+v, err=%v", results, err)
	}
	cover, err := b.Fetch(ctx, "demo:cover:paper")
	if err != nil || cover.Bounds().Dx() != 320 || cover.Bounds().Dy() != 320 {
		t.Fatalf("cover bounds = %v, err=%v", cover.Bounds(), err)
	}
	if cover.At(0, 0) == cover.At(160, 160) {
		t.Fatal("generated cover has no variation")
	}
}
