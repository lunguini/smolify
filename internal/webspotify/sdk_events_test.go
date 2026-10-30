//go:build !js || !wasm

package webspotify

import (
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

func TestMapSDKState(t *testing.T) {
	state := SDKState{
		Track:      model.Track{URI: "spotify:track:abc", Name: "A demo song"},
		ContextURI: "spotify:album:xyz", Position: 17 * time.Second,
		Duration: 200 * time.Second,
	}
	events := MapSDKState(state)
	if len(events) != 3 {
		t.Fatalf("event count = %d, events=%#v", len(events), events)
	}
	if got, ok := events[0].(model.Track); !ok || got.ContextURI != state.ContextURI || got.Position != state.Position {
		t.Fatalf("track event = %#v", events[0])
	}
	if got, ok := events[1].(model.Seek); !ok || got.Position != state.Position || got.Duration != state.Duration {
		t.Fatalf("seek event = %#v", events[1])
	}
	if got, ok := events[2].(model.Playing); !ok || got.URI != state.Track.URI {
		t.Fatalf("playing event = %#v", events[2])
	}
	paused := MapSDKState(SDKState{Track: state.Track, Paused: true})
	if _, ok := paused[2].(model.Paused); !ok {
		t.Fatalf("paused event = %#v", paused[2])
	}
	if stopped := MapSDKState(SDKState{Empty: true}); len(stopped) != 1 {
		t.Fatalf("empty state = %#v", stopped)
	} else if _, ok := stopped[0].(model.Stopped); !ok {
		t.Fatalf("empty state event = %#v", stopped[0])
	}
}

func TestMapSDKAccountError(t *testing.T) {
	got := MapSDKError(SDKAccountError, "Premium is required")
	err, ok := got.(model.PlaybackError)
	if !ok || err.Kind != "account" || err.Message != "Premium is required" {
		t.Fatalf("mapped error = %#v", got)
	}
}
