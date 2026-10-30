//go:build js && wasm

package app

import "syscall/js"

// openURL uses the browser directly so popup blockers can associate it with
// the user's click on the pairing action.
func openURL(s *State, url string) {
	if url == "" {
		return
	}
	window := js.Global().Get("window")
	if window.IsUndefined() || window.IsNull() {
		fail(s, "Could not open the browser")
		return
	}
	opened := window.Call("open", url, "_blank", "noopener")
	if opened.IsNull() || opened.IsUndefined() {
		fail(s, "The browser blocked the new tab")
	}
}
