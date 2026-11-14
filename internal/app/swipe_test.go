package app

import (
	"testing"
	"time"

	"github.com/lunguini/flatte"
)

func TestSwipeNavigatesOncePerSwipe(t *testing.T) {
	var w swipe
	start := time.Unix(100, 0)
	var got []string
	for i := range 3 * swipeNotches { // one long swipe, momentum included
		if cmd, ok := swipeStep(&w, true, start.Add(time.Duration(i)*10*time.Millisecond)); ok {
			got = append(got, cmd)
		}
	}
	if len(got) != 1 || got[0] != cmdBack {
		t.Fatalf("swipe right = %v, want one back", got)
	}
	later := start.Add(time.Second)
	for i := range swipeNotches {
		if cmd, ok := swipeStep(&w, false, later.Add(time.Duration(i)*10*time.Millisecond)); ok {
			got = append(got, cmd)
		}
	}
	if len(got) != 2 || got[1] != cmdForward {
		t.Fatalf("then a swipe left = %v, want forward", got)
	}
}

func TestSwipeIgnoresANudge(t *testing.T) {
	var w swipe
	start := time.Unix(100, 0)
	for i := range swipeNotches - 1 {
		if _, ok := swipeStep(&w, true, start.Add(time.Duration(i)*10*time.Millisecond)); ok {
			t.Fatal("a short sideways scroll navigated")
		}
	}
	// After a pause, the count starts over.
	if _, ok := swipeStep(&w, true, start.Add(time.Second)); ok {
		t.Fatal("a nudge after a pause completed the earlier swipe")
	}
}

func TestSideButtonsAndSwipesGoBackAndForward(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	d.Send(flatte.MenuEvent{ID: cmdHome})
	d.Settle()
	d.Send(flatte.MouseEvent{Button: flatte.MouseBack, Action: flatte.MousePress})
	if s.page != pageLibrary {
		t.Fatalf("back button: page %v", s.page)
	}
	d.Send(flatte.MouseEvent{Button: flatte.MouseForward, Action: flatte.MousePress})
	if s.page != pageHome {
		t.Fatalf("forward button: page %v", s.page)
	}
	for range swipeNotches {
		d.Send(flatte.MouseEvent{Button: flatte.MouseWheelLeft, Action: flatte.MousePress})
	}
	if s.page != pageLibrary {
		t.Fatalf("swipe right: page %v", s.page)
	}
}
