package app

import (
	"context"
	"os/exec"

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
	}}}
}

func playLabel(s *State) string {
	if s.playing {
		return "Pause"
	}
	return "Play"
}

// ContextMenu is the menu for a playlist in the library.
func ContextMenu(s *State, target string) []flatte.MenuItem {
	i, ok := railIndex(target)
	if !ok || i >= len(s.playlists) {
		return nil
	}
	return []flatte.MenuItem{{ID: cmdPlayList, Label: "Play “" + s.playlists[i].Name + "”"}}
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
