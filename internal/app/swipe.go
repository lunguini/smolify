package app

import (
	"time"

	"github.com/lunguini/flatte"
)

// A sideways swipe on a trackpad reaches a terminal as horizontal wheel
// notches, a burst per swipe. Enough of them one way go back or forward, as
// swiping between pages does in a browser; the rest of the burst, momentum
// included, is spent before the next swipe counts.

const (
	// swipeNotches is how far a swipe goes before it navigates.
	swipeNotches = 6
	// swipeGap is the pause that ends a swipe.
	swipeGap = 350 * time.Millisecond
)

// swipe is the sideways swipe in progress.
type swipe struct {
	notches int // signed: left is negative
	last    time.Time
	done    bool // it has navigated
}

// swipeStep adds a horizontal wheel notch at now to the swipe in progress
// and reports the command it completes, if any: back for a swipe right
// (notches left), forward for a swipe left.
func swipeStep(w *swipe, left bool, now time.Time) (string, bool) {
	step := 1
	if left {
		step = -1
	}
	if now.Sub(w.last) > swipeGap || w.notches*step < 0 {
		*w = swipe{}
	}
	w.last = now
	w.notches += step
	if w.done || max(w.notches, -w.notches) < swipeNotches {
		return "", false
	}
	w.done = true
	if left {
		return cmdBack, true
	}
	return cmdForward, true
}

// handleSwipe navigates on a mouse's side buttons and on sideways swipes.
func handleSwipe(s *State, m flatte.MouseEvent, fx flatte.Effects[State]) bool {
	switch m.Button {
	case flatte.MouseBack:
		if m.Action == flatte.MousePress {
			command(s, cmdBack, fx)
		}
	case flatte.MouseForward:
		if m.Action == flatte.MousePress {
			command(s, cmdForward, fx)
		}
	case flatte.MouseWheelLeft, flatte.MouseWheelRight:
		if cmd, ok := swipeStep(&s.swipe, m.Button == flatte.MouseWheelLeft, time.Now()); ok {
			command(s, cmd, fx)
		}
	default:
		return false
	}
	return true
}
