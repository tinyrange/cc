// Package guestinit implements the Linux cc and SA2 guest init entrypoints.
// Non-Linux entrypoints compile but panic if invoked.
package guestinit

// SandboxProtocol is the SA2 guest protocol supported by this init.
const SandboxProtocol = 1
