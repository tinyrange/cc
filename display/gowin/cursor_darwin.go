//go:build darwin

package gowin

import "github.com/ebitengine/purego/objc"

// Visibility alone preserves absolute mouse motion. Gowin's cursor capture
// also disconnects mouse motion from the host cursor, which is for relative
// input and must not be used for this absolute-pointer presentation.
func cursorVisibility() func(bool) {
	hidden := false
	cursor := objc.ID(objc.GetClass("NSCursor"))
	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	return func(wantHidden bool) {
		wantHidden = wantHidden && objc.Send[bool](app, objc.RegisterName("isActive"))
		if wantHidden == hidden {
			return
		}
		if wantHidden {
			cursor.Send(objc.RegisterName("hide"))
		} else {
			cursor.Send(objc.RegisterName("unhide"))
		}
		hidden = wantHidden
	}
}
