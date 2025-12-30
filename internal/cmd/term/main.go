package main

import (
	"runtime"

	"github.com/tinyrange/cc/internal/term"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t, err := term.New("term", 1024, 768)
	if err != nil {
		panic(err)
	}
	defer t.Close()

	pty, err := startLoginShell()
	if err != nil {
		panic(err)
	}
	defer pty.Close()

	// PTY -> Terminal (output)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				_, _ = t.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Terminal -> PTY (input).
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := t.Read(buf)
			if n > 0 {
				b := buf[:n]
				for len(b) > 0 {
					w, werr := pty.Write(b)
					if w > 0 {
						b = b[w:]
					}
					if werr != nil || w == 0 {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	if err := t.Run(nil, term.Hooks{
		OnResize: func(cols, rows int) {
			_ = pty.Resize(cols, rows)
		},
	}); err != nil {
		panic(err)
	}
}
