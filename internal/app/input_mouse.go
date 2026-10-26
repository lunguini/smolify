package app

import (
	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
)

// handleFieldMouse keeps edits on the loop and applies each field's existing
// change policy. Selection alone never starts a search or saves settings.
func handleFieldMouse(s *State, m flatte.MouseEvent) bool {
	if m.Button != flatte.MouseLeft || (!m.HasCaret && !m.Clear) {
		return false
	}
	switch m.Target {
	case idQuery:
		if m.Action == flatte.MousePress {
			s.trackSort.Close()
			focusSearch(s)
		}
		before := s.query.Value
		if !s.query.HandleMouse(idQuery, m) {
			return false
		}
		if s.query.Value != before {
			queueSearch(s)
		}
		return true
	case idTrackFilter:
		if m.Action == flatte.MousePress {
			openTrackFilter(s)
		}
		handled := false
		changeTrackFilter(s, func(field *flatui.TextField) { handled = field.HandleMouse(idTrackFilter, m) })
		return handled
	case setDeviceName + ":field":
		if m.Action == flatte.MousePress {
			s.trackSort.Close()
			s.focus = paneDevice
		}
		return s.deviceName.HandleMouse(setDeviceName+":field", m)
	}
	return false
}
