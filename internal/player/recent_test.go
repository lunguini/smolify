package player

import (
	"strings"
	"testing"
	"time"
)

func TestParseRecent(t *testing.T) {
	got, err := parseRecent(strings.NewReader(`{"playContexts":[
		{"uri":"spotify:playlist:a","lastPlayedTime":1792526735384,"lastPlayedTrackUri":"spotify:track:1"},
		{"uri":"","lastPlayedTime":1,"lastPlayedTrackUri":"spotify:track:2"},
		{"uri":"spotify:album:b","lastPlayedTime":1792485636334,"lastPlayedTrackUri":"spotify:track:3"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Played{
		{ContextURI: "spotify:playlist:a", TrackURI: "spotify:track:1", At: time.UnixMilli(1792526735384)},
		{ContextURI: "spotify:album:b", TrackURI: "spotify:track:3", At: time.UnixMilli(1792485636334)},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	if _, err := parseRecent(strings.NewReader(`{`)); err == nil {
		t.Fatal("a broken answer parsed")
	}
}

func TestAudioBitrate(t *testing.T) {
	for in, want := range map[int]int{0: 320, 96: 96, 160: 160, 320: 320, 128: 320} {
		if got := (Audio{Bitrate: in}).bitrate(); got != want {
			t.Errorf("bitrate %d → %d, want %d", in, got, want)
		}
	}
}
