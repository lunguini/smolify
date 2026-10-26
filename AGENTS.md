# Smolify Development Guide

Smolify is a lightweight Spotify client — terminal by default, a native
window with `-gui` — built on Flatte (`github.com/lunguini/flatte`, branch
`gui`) as an outside module, with playback by an embedded go-librespot.
It is GPL-3.0 (go-librespot is). It needs Spotify Premium.

## Internal docs

Plans, status, and findings live in `.docs/` (git-crypt encrypted, opaque
names). Read `.docs/index.md` first. If `.docs` files look binary, stop and
ask for `git-crypt unlock`. Do not copy `.docs` content into public files or
commit messages.

## Build

Needs cgo and Homebrew's audio codecs:

```bash
brew install libogg libvorbis flac mpg123
go test ./...
go run ./cmd/smolify          # terminal
go run ./cmd/smolify -gui     # window (macOS)
```

The player stores its device ID and credentials in the user config
directory (`smolify/state.json`, mode 0600). Never commit credentials.

## Shape

- `cmd/` — process setup only.
- `internal/player` — the embedded player; no UI types.
- `internal/app` — Flatte state, handlers, and pure views, with tests and
  goldens.
- Flatte conventions apply: one state struct, pure View, no messages, async
  results as named updates, layout trees for frames.

Commits follow Conventional Commits; no co-author trailers.

## UI and UX conventions

- Keep secondary toolbar actions compact. Reveal local search from an icon;
  focus it on opening, and keep a nonempty filter visible after focus moves.
  A local field has a bounded width; only primary page content grows freely.
- Controls sharing a toolbar use the native theme's regular height (32pt
  by default), aligned on their center line. Prefer Auto sizing to inherit
  it; explicit sizes are for intentional exceptions. Group related controls, preserve breathing room,
  and use explicit padding exceptions for edge-to-edge content.
- Keep descriptions close to their title; put flexible space before actions.
  Give list rows vertical room without enlarging their text unnecessarily.
- Icon actions need tooltips and keyboard access. Test focus order, native
  editing, Escape, click-away, clearing, and empty results in both hosts.
- Give immediate feedback for requested actions, distinguish loading from
  confirmed playback, and preserve the selected song across sort/filter
  updates. Background events must not move selection in another list.
- Use clear sorting labels and real data. Never label affinity, popularity,
  incomplete recent history, or app-local observations as account play counts.
- Use motion to clarify state when supported by existing primitives; keep
  state changes understandable without animation. Avoid layout animation
  infrastructure solely for decoration.
- Check new UI at narrow and normal sizes, inspect native snapshots, and
  verify actual hit targets and keyboard behavior before calling it done.

- A background click dismisses transient controls and returns keyboard
  focus to Play/Pause. Explicit clicks on rows, fields or other controls
  retain their intent. Blurred fields/dropdowns must not consume Space.

- Text inputs use TextField.HandleMouse for native pointer selection;
  clearable fields route clearing through their normal change handler.
  Existing slider/scroll gestures retain ownership until release. Keep
  fallback artwork glyphs centered in their native tiles.
