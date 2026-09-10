//go:build !darwin

package gowin

func cursorVisibility() func(bool) { return func(bool) {} }
