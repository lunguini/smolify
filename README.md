# Smolify

A small, fast Spotify client for the terminal and the desktop, built with
[Flatte](https://github.com/lunguini/flatte) and
[go-librespot](https://github.com/devgianlu/go-librespot).

Work in progress. Requires Spotify Premium. Not affiliated with Spotify.

## Install

With Go 1.26 or later, until there are Homebrew and release builds:

```bash
brew install libogg libvorbis flac mpg123   # audio codecs (cgo)
go install github.com/lunguini/smolify/cmd/smolify@latest
smolify          # in the terminal
smolify -gui     # in a window (macOS)
```

`go install` puts `smolify` in `$(go env GOPATH)/bin` (usually `~/go/bin`);
add that to your `PATH` if the command is not found. Run the same
`go install` again to update.

While the repository is private, Go has to fetch it with your GitHub access
instead of the public module proxy:

```bash
GOPRIVATE=github.com/lunguini go install github.com/lunguini/smolify/cmd/smolify@latest
```

On first run, Smolify shows a code: open spotify.com/pair and enter it to
sign in.

## Build

```bash
brew install libogg libvorbis flac mpg123   # audio codecs (cgo)
go run ./cmd/smolify                        # in the terminal
go run ./cmd/smolify -gui                   # in a window (macOS)
go run ./cmd/smolify -sign-out              # forget the Spotify sign-in
go run ./cmd/smolify -debug-log smolify.log # log player events and memory
```

## License

GPL-3.0 — see [LICENSE](LICENSE).

The bundled Inter font (`internal/app/fonts`) is under the SIL Open Font
License 1.1 — see [its license](internal/app/fonts/OFL.txt).
