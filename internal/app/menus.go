package app

import (
	"context"
	"os/exec"
	"strings"

	"github.com/lunguini/flatte"
)

func key(k flatte.Key, mod flatte.Mod) flatte.KeyEvent { return flatte.KeyEvent{Key: k, Mod: mod} }

func char(r rune, mod flatte.Mod) flatte.KeyEvent {
	return flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: r, Mod: mod}
}

// Menus is the menu bar; its shortcuts are Smolify's playback keys, in every
// host (plain keys for a terminal, ⌘ chords too in a window).
func Menus(s *State) []flatte.Menu {
	_, hasPlaylist := s.selected()
	noTrack := s.track == nil
	song := !noTrack && strings.HasPrefix(s.track.URI, "spotify:track:")
	liked := !noTrack && s.liked[s.track.URI]
	return []flatte.Menu{{Title: "Playback", Items: []flatte.MenuItem{
		{ID: cmdPlayPause, Label: playLabel(s), Keys: []flatte.KeyEvent{char(' ', 0)}, Disabled: noTrack && !hasPlaylist},
		{ID: cmdNext, Label: "Next", Keys: []flatte.KeyEvent{key(flatte.KeyRight, flatte.ModSuper), key(flatte.KeyRight, flatte.ModCtrl), char('n', 0)}, Disabled: noTrack},
		{ID: cmdPrev, Label: "Previous", Keys: []flatte.KeyEvent{key(flatte.KeyLeft, flatte.ModSuper), key(flatte.KeyLeft, flatte.ModCtrl), char('p', 0)}, Disabled: noTrack},
		{Separator: true},
		{ID: cmdSeekAhead, Label: "Seek Forward", Keys: []flatte.KeyEvent{key(flatte.KeyRight, 0)}, Disabled: noTrack},
		{ID: cmdSeekBack, Label: "Seek Backward", Keys: []flatte.KeyEvent{key(flatte.KeyLeft, 0)}, Disabled: noTrack},
		{Separator: true},
		{ID: cmdVolumeUp, Label: "Volume Up", Keys: []flatte.KeyEvent{key(flatte.KeyUp, flatte.ModSuper), char('+', 0), char('=', 0)}},
		{ID: cmdVolumeDown, Label: "Volume Down", Keys: []flatte.KeyEvent{key(flatte.KeyDown, flatte.ModSuper), char('-', 0)}},
		{Separator: true},
		{ID: cmdShuffle, Label: "Shuffle", Checked: s.shuffle, Keys: []flatte.KeyEvent{char('s', 0)}},
		{ID: cmdRepeat, Label: "Repeat", Checked: s.repeat.Context, Keys: []flatte.KeyEvent{char('r', 0)}},
		{Separator: true},
		{ID: cmdLike, Label: likeLabel(liked), Keys: []flatte.KeyEvent{char('l', 0)}, Disabled: !song},
	}}}
}

func playLabel(s *State) string {
	if s.playing {
		return "Pause"
	}
	return "Play"
}

// ContextMenu is the menu for a list in the library or a track in the
// table.
func ContextMenu(s *State, target string) []flatte.MenuItem {
	if i, ok := rowIndex(idRail, target); ok {
		lib := s.library()
		if i >= len(lib) {
			return nil
		}
		return []flatte.MenuItem{{ID: cmdPlayList, Label: "Play “" + lib[i].Name + "”"}}
	}
	i, ok := rowIndex(idTracks, target)
	l := s.listings[s.shown]
	if !ok || i >= len(l.Tracks) {
		return nil
	}
	t := l.Tracks[i]
	song := strings.HasPrefix(t.URI, "spotify:track:")
	return []flatte.MenuItem{
		{ID: cmdPlayTrack, Label: "Play"},
		{ID: cmdQueue, Label: "Add to Queue"},
		{Separator: true},
		{ID: cmdLikeTrack, Label: likeLabel(s.liked[t.URI]), Disabled: !song},
	}
}

func likeLabel(liked bool) string {
	if liked {
		return "Remove from Liked Songs"
	}
	return "Save to Liked Songs"
}

// openURL opens url in the default browser.
func openURL(s *State, url string) {
	if url == "" {
		return
	}
	flatte.Go(s.fx, "open-url", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, exec.CommandContext(ctx, "open", url).Run()
	}, func(s *State, _ struct{}, err error) {
		if err != nil {
			s.status = "Could not open the browser: " + err.Error()
		}
	})
}
