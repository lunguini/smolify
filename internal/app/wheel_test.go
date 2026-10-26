package app

import (
	"testing"

	"github.com/lunguini/flatte"
)

func wheelEvent(target string, button flatte.MouseButton) flatte.MouseEvent {
	return flatte.MouseEvent{Target: target, Button: button, Action: flatte.MousePress}
}

func TestWheelScrollsOnlySemanticTargets(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.rail.SetCount(100)
	s.tracks.SetCount(100)
	s.discovery.HomeMax = 20

	rail, tracks := s.rail.Offset(), s.tracks.Offset()
	for _, target := range []string{"background", "center-card", "playlist-header", idPlay, idSearch} {
		wheel(s, wheelEvent(target, flatte.MouseWheelDown), s.fx)
	}
	if s.rail.Offset() != rail || s.tracks.Offset() != tracks {
		t.Fatalf("non-scrollable targets moved lists: rail %d tracks %d", s.rail.Offset(), s.tracks.Offset())
	}
	wheel(s, wheelEvent(idRail+":0", flatte.MouseWheelDown), s.fx)
	if s.rail.Offset() != rail+1 || s.tracks.Offset() != tracks {
		t.Fatalf("rail row wheel offsets: rail %d tracks %d", s.rail.Offset(), s.tracks.Offset())
	}
	wheel(s, wheelEvent(idTracks+":0", flatte.MouseWheelDown), s.fx)
	if s.tracks.Offset() != tracks+1 {
		t.Fatalf("track row wheel offset %d, want %d", s.tracks.Offset(), tracks+1)
	}

	before := s.tracks.Offset()
	s.page = pageHome
	wheel(s, wheelEvent("browse:spotify:artist:someone", flatte.MouseWheelDown), s.fx)
	if s.tracks.Offset() != before {
		t.Fatal("browse link outside a track page scrolled tracks")
	}
	s.page = pageSearch
	wheel(s, wheelEvent("browse:spotify:artist:someone", flatte.MouseWheelDown), s.fx)
	if s.tracks.Offset() != before+1 {
		t.Fatal("metadata link in Search did not scroll its track list")
	}

	s.page = pageHome
	s.discovery.HomeScroll = 2
	wheel(s, wheelEvent("home-card:0", flatte.MouseWheelDown), s.fx)
	if s.discovery.HomeScroll != 5 {
		t.Fatalf("Home card wheel scroll=%d, want 5", s.discovery.HomeScroll)
	}
	wheel(s, wheelEvent("browse:spotify:album:ignored", flatte.MouseWheelDown), s.fx)
	if s.discovery.HomeScroll != 5 {
		t.Fatal("unscoped browse target scrolled Home")
	}

	s.page = pageSettings
	s.settingsScroll, s.settingsMax = 3, 10
	wheel(s, wheelEvent("background", flatte.MouseWheelDown), s.fx)
	if s.settingsScroll != 3 {
		t.Fatal("background wheel scrolled Settings")
	}
	wheel(s, wheelEvent(setApplyAudio+":button", flatte.MouseWheelDown), s.fx)
	if s.settingsScroll != 5 {
		t.Fatalf("Settings control wheel scroll=%d, want 5", s.settingsScroll)
	}
}

func TestDropdownWheelRequiresPopupTarget(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.trackSort.Select(0)
	s.trackSort.Open()
	trackHighlight := s.trackSort.Highlighted()
	handleMouse(s, wheelEvent("background", flatte.MouseWheelDown), s.fx)
	if s.trackSort.Highlighted() != trackHighlight {
		t.Fatal("wheel outside sort popup moved its selection")
	}
	handleMouse(s, wheelEvent(idSort+":option:1", flatte.MouseWheelDown), s.fx)
	if s.trackSort.Highlighted() != trackHighlight+1 {
		t.Fatal("wheel on sort option did not move its highlight")
	}

	s.trackSort.Close()
	s.page = pageSettings
	s.settingsScroll, s.settingsMax = 4, 10
	s.settingQuality.Select(0)
	s.settingQuality.Open()
	qualityHighlight := s.settingQuality.Highlighted()
	handleMouse(s, wheelEvent("background", flatte.MouseWheelDown), s.fx)
	handleMouse(s, wheelEvent(idVolume, flatte.MouseWheelDown), s.fx)
	if s.settingsScroll != 4 || s.settingQuality.Highlighted() != qualityHighlight {
		t.Fatal("wheel outside settings popup affected the form or dropdown")
	}
	handleMouse(s, wheelEvent(setQuality+":select:option:1", flatte.MouseWheelDown), s.fx)
	if s.settingQuality.Highlighted() != qualityHighlight+1 {
		t.Fatal("wheel on settings option did not move its highlight")
	}
}
