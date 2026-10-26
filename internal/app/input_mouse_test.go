package app

import (
	"github.com/lunguini/flatte"
	"strings"
	"testing"
)

func inputPress(target string, caret, clicks int) flatte.MouseEvent {
	return flatte.MouseEvent{Target: target, Button: flatte.MouseLeft, Action: flatte.MousePress, HasCaret: true, Caret: caret, Clicks: clicks}
}
func TestSearchFieldsApplyWordSelectionAndClear(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	d.Send(flatte.PasteEvent{Text: "Daft Punk"})
	d.Settle()
	Handle(s, inputPress(idQuery, 2, 2), s.fx)
	if s.query.SelectedText() != "Daft" {
		t.Fatalf("global search selected %q", s.query.SelectedText())
	}
	Handle(s, inputPress(idQuery, 1, 3), s.fx)
	if s.query.SelectedText() != "Daft Punk" {
		t.Fatal("triple click did not select all")
	}
	Handle(s, flatte.MouseEvent{Target: idQuery, Button: flatte.MouseLeft, Action: flatte.MousePress, Clear: true}, s.fx)
	d.Settle()
	if s.query.Value != "" || s.page == pageSearch || s.focus != paneSearch {
		t.Fatal("global clear did not restore prior page with field focused")
	}
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	d.Send(flatte.PasteEvent{Text: "Gravity"})
	d.Settle()
	Handle(s, inputPress(idTrackFilter, 3, 2), s.fx)
	if s.trackFilter.SelectedText() != "Gravity" {
		t.Fatal("local filter has no word selection")
	}
	Handle(s, flatte.MouseEvent{Target: idTrackFilter, Button: flatte.MouseLeft, Action: flatte.MousePress, Clear: true}, s.fx)
	if s.trackFilter.Value != "" || s.tracks.Count() != 3 || s.focus != paneTrackFilter {
		t.Fatal("local clear did not restore all songs")
	}
}
func TestDeviceFieldMouseEditsStayDraftUntilEnter(t *testing.T) {
	d, f, _ := settingsDriver(t)
	s := d.State()
	s.deviceName.Value = "Desk Player"
	Handle(s, inputPress(setDeviceName+":field", 2, 2), s.fx)
	if s.deviceName.SelectedText() != "Desk" || s.focus != paneDevice {
		t.Fatal("device field mouse selection missing")
	}
	f.calls = nil
	Handle(s, flatte.MouseEvent{Target: setDeviceName + ":field", Button: flatte.MouseLeft, Action: flatte.MousePress, Clear: true}, s.fx)
	d.Settle()
	if s.deviceName.Value != "" || len(f.calls) != 0 {
		t.Fatal("device clear was not a draft edit")
	}
}

func TestSliderReleaseOverInputKeepsGestureOwnership(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	event(d, song)
	s.query.Value = "untouched"
	f.calls = nil
	s.drag = idProgress
	r := s.sliders[idProgress]
	Handle(s, flatte.MouseEvent{Target: idQuery, Button: flatte.MouseLeft, Action: flatte.MouseRelease, HasCaret: true, Caret: 2, X: r.X + r.W/2}, s.fx)
	d.Settle()
	if s.drag != "" || s.query.Value != "untouched" || len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "seek ") {
		t.Fatalf("release lost to field: drag %q query %q calls %v", s.drag, s.query.Value, f.calls)
	}
}
