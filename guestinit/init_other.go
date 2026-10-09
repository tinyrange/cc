//go:build !linux

// Package guestinit implements the Linux guest init and its helper protocol.
package guestinit

func Run()        { panic("guestinit requires Linux") }
func RunSandbox() { panic("guestinit requires Linux") }
