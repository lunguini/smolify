package player

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
)

func TestEventOf(t *testing.T) {
	cover := "https://i.scdn.co/image/x"
	var repeat Repeat
	for _, c := range []struct {
		in   *daemon.ApiEvent
		want Event
	}{
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeMetadata, Data: daemon.ApiEventDataMetadata{
			Uri: "spotify:track:1", Name: "Song", ArtistNames: []string{"A", "B"}, AlbumName: "Album",
			AlbumCoverUrl: &cover, Duration: 185000, Position: 1500,
		}}, nil},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypePlaying, Data: daemon.ApiEventDataPlaying{Uri: "u", ContextUri: "c"}}, Playing{URI: "u", ContextURI: "c"}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypePaused, Data: daemon.ApiEventDataPaused{Uri: "u"}}, Paused{URI: "u"}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeStopped, Data: daemon.ApiEventDataStopped{}}, Stopped{}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeNotPlaying, Data: daemon.ApiEventDataNotPlaying{}}, Stopped{}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeSeek, Data: daemon.ApiEventDataSeek{Position: 2000, Duration: 9000}}, Seek{Position: 2 * time.Second, Duration: 9 * time.Second}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeVolume, Data: daemon.ApiEventDataVolume{Value: 40, Max: 100}}, Volume{Value: 40, Max: 100}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeShuffleContext, Data: daemon.ApiEventDataShuffleContext{Value: true}}, Shuffle{On: true}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeRepeatContext, Data: daemon.ApiEventDataRepeatContext{Value: true}}, Repeat{Context: true}},
		// The player reports one mode at a time; the event carries both.
		{&daemon.ApiEvent{Type: daemon.ApiEventTypeRepeatTrack, Data: daemon.ApiEventDataRepeatTrack{Value: true}}, Repeat{Context: true, Track: true}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypePlaybackError, Data: daemon.ApiEventDataPlaybackError{Uri: "u", Kind: "k", Unplayable: true}}, PlaybackError{URI: "u", Kind: "k", Unplayable: true}},
		{&daemon.ApiEvent{Type: daemon.ApiEventTypePlaybackReady}, Ready{}},
	} {
		got, ok := eventOf(c.in, &repeat)
		if !ok {
			t.Errorf("%s: ignored", c.in.Type)
			continue
		}
		if c.want == nil { // the track: compared field by field below
			tr := got.(Track)
			if tr.URI != "spotify:track:1" || tr.Name != "Song" || len(tr.Artists) != 2 || tr.Album != "Album" ||
				tr.Cover != cover || tr.Duration != 185*time.Second || tr.Position != 1500*time.Millisecond {
				t.Errorf("track %+v", tr)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: %#v, want %#v", c.in.Type, got, c.want)
		}
	}
	if _, ok := eventOf(&daemon.ApiEvent{Type: daemon.ApiEventTypeActive}, &repeat); ok {
		t.Error("an unused event was converted")
	}
}

func TestPairingEvents(t *testing.T) {
	var got []Event
	s := &server{emit: func(e Event) { got = append(got, e) }}
	exp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.SetAuthCode(&daemon.ApiDeviceAuth{Url: "https://spotify.com/pair?code=AB", Code: "AB", ExpiresAt: exp})
	s.SetAuthCode(nil)
	if len(got) != 2 || got[0] != (Pairing{URL: "https://spotify.com/pair?code=AB", Code: "AB", Expires: exp}) || got[1] != (Pairing{}) {
		t.Fatalf("pairing events %#v", got)
	}
}

func TestFileStoreAndSignOut(t *testing.T) {
	dir := t.TempDir()
	f := fileStore{dir: dir}
	st, err := f.Load()
	if err != nil || st.DeviceId != "" {
		t.Fatalf("empty load: %+v %v", st, err)
	}
	st = &librespot.AppState{DeviceId: "dev"}
	st.Credentials.Username, st.Credentials.Data = "me", []byte("secret")
	if err := f.Save(st); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file %v %v", info, err)
	}
	back, err := f.Load()
	if err != nil || back.DeviceId != "dev" || back.Credentials.Username != "me" || string(back.Credentials.Data) != "secret" {
		t.Fatalf("round trip %+v %v", back, err)
	}
	if err := SignOut(dir); err != nil {
		t.Fatal(err)
	}
	back, _ = f.Load()
	if back.DeviceId != "dev" || back.Credentials.Username != "" || back.Credentials.Data != nil {
		t.Fatalf("after sign out %+v", back)
	}
}

func TestListingOf(t *testing.T) {
	cover := "https://img/1"
	l := listingOf(&daemon.ApiContextTracks{Uri: "spotify:playlist:a", Ready: true, Length: 2, Cached: 1, Tracks: []daemon.ApiContextTrackItem{
		{Uri: "spotify:track:1", Track: &daemon.ApiTrack{Uri: "spotify:track:1", Name: "One", ArtistNames: []string{"A"}, AlbumName: "Al", AlbumCoverUrl: &cover, Duration: 1500}},
		{Uri: "spotify:track:2"},
	}})
	if l.URI != "spotify:playlist:a" || !l.Ready || l.Length != 2 || l.Cached != 1 || len(l.Tracks) != 2 || l.Complete() {
		t.Fatalf("listing %+v", l)
	}
	if one := l.Tracks[0]; one.Name != "One" || one.Album != "Al" || one.Cover != cover || one.Duration != 1500*time.Millisecond {
		t.Fatalf("track %+v", one)
	}
	if two := l.Tracks[1]; two.URI != "spotify:track:2" || two.Name != "" {
		t.Fatalf("unresolved %+v", two)
	}
	if !(Listing{Ready: true, Length: 2, Cached: 2}).Complete() || (Listing{Length: 0}).Complete() {
		t.Fatal("Complete")
	}
	if LikedSongs("adrian") != "spotify:user:adrian:collection" {
		t.Fatal(LikedSongs("adrian"))
	}
}
