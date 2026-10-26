package app

import (
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
)

var linkMetrics = layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32}

func nativeSceneIDsAt(t *testing.T, s *State, id string) []string {
	t.Helper()
	root := View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root
	scene := layout.LayoutScene(root, 1100, 800, linkMetrics)
	r, ok := scene.Rects[id]
	if !ok || r.W == 0 || r.H == 0 {
		t.Fatalf("native scene has no visible target %q: %v", id, r)
	}
	ids := scene.IDsAt(r.X+r.W/2, r.Y+r.H/2)
	found := false
	for _, got := range ids {
		found = found || got == id
	}
	if !found {
		t.Fatalf("native hit at %q returned %v", id, ids)
	}
	return ids
}

func TestTrackArtistLinksTargetExactArtistAndBrowseWithoutPlaying(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "two-line", true: "compact"}[compact], func(t *testing.T) {
			d, f := ready(t)
			s := d.State()
			s.compact = compact
			track := song
			track.ArtistURIs = []string{"spotify:artist:kuo", "spotify:artist:lin"}
			listing := s.listings[s.shown]
			listing.Tracks[0] = track
			s.listings[s.shown] = listing
			target := trackLinkID("spotify:artist:lin", 0, "1")
			ids := nativeSceneIDsAt(t, s, target)
			if !containsString(ids, idTracks+":0") {
				t.Fatalf("artist hit did not belong to its track row: %v", ids)
			}
			from := s.here()
			Handle(s, flatte.MouseEvent{Target: target, Button: flatte.MouseLeft, Action: flatte.MousePress}, s.fx)
			d.Settle()
			if s.page != pageBrowse || s.shown != "spotify:artist:lin" {
				t.Fatalf("artist click did not open its exact artist: page=%v shown=%q", s.page, s.shown)
			}
			if len(s.back) == 0 || s.back[len(s.back)-1] != from {
				t.Fatalf("browse history did not retain source place: %v", s.back)
			}
			for _, call := range f.calls {
				if len(call) >= 5 && call[:5] == "play " {
					t.Fatalf("artist click played a row: %v", f.calls)
				}
			}
		})
	}
}

func TestTrackAlbumLinkAndMissingURIs(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	track := song
	track.AlbumURI = "spotify:album:thinking"
	track.ArtistURIs = []string{"spotify:artist:kuo"} // Lin has no URI and stays plain text.
	listing := s.listings[s.shown]
	listing.Tracks[0] = track
	s.listings[s.shown] = listing
	target := trackLinkID("spotify:album:thinking", 0, "album")
	ids := nativeSceneIDsAt(t, s, target)
	if !containsString(ids, idTracks+":0") {
		t.Fatalf("album hit did not belong to its track row: %v", ids)
	}
	Handle(s, flatte.MouseEvent{Target: target, Button: flatte.MouseLeft, Action: flatte.MousePress}, s.fx)
	d.Settle()
	if s.page != pageBrowse || s.shown != "spotify:album:thinking" {
		t.Fatalf("album click did not open its album: page=%v shown=%q", s.page, s.shown)
	}

	d2, _ := ready(t)
	s2 := d2.State()
	track.AlbumURI = ""
	track.ArtistURIs = []string{"spotify:artist:kuo"}
	listing = s2.listings[s2.shown]
	listing.Tracks[0] = track
	s2.listings[s2.shown] = listing
	scene := layout.LayoutScene(View(s2, flatte.RenderContext{Width: 110, Graphical: true}).Root, 1100, 800, linkMetrics)
	if _, ok := scene.Rects[trackLinkID("spotify:album:thinking", 0, "album")]; ok {
		t.Fatal("album label without a URI became clickable")
	}
	if _, ok := scene.Rects[trackLinkID("spotify:artist:lin", 0, "1")]; ok {
		t.Fatal("artist label without a matching URI became clickable")
	}
	if _, ok := scene.Rects[trackLinkID("spotify:artist:kuo", 0, "0")]; !ok {
		t.Fatal("artist with a matching URI did not become clickable")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestLinkedArtistLineFitsInsideNativeSongRow(t *testing.T) {
	s := sampleApp().State
	track := song
	track.ArtistURIs = []string{"spotify:artist:kuo", "spotify:artist:lin"}
	m := linkMetrics
	m.MeasureText = func(text string, style layout.TextStyle, _ float64) (float64, float64) {
		if text == "" {
			return 0, 0
		}
		size := style.Size
		if size == 0 {
			size = 14
		}
		leading := style.Leading
		if leading == 0 {
			leading = 1.35
		}
		return float64(len([]rune(text))) * size / 2, size * 1.2 * leading
	}
	scene := layout.LayoutScene(trackRow(s, 0, track, true, false), 600, 48, m)
	for _, value := range scene.Ops {
		text, ok := value.(layout.TextOp)
		if !ok || text.Text != "Kuo" {
			continue
		}
		_, needed := m.MeasureText(text.Text, text.Style, 0)
		visible := min(text.Rect.Y+text.Rect.H, text.Clip.Y+text.Clip.H) - max(text.Rect.Y, text.Clip.Y)
		if visible+0.01 < needed {
			t.Fatalf("artist line clipped: visible=%g needed=%g rect=%+v clip=%+v", visible, needed, text.Rect, text.Clip)
		}
		return
	}
	t.Fatal("Artist line missing")
}
