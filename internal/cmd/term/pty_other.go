//go:build !darwin

package main

import (
	"errors"
	"io"
)

type PtyShell struct{}

func startLoginShell() (*PtyShell, error) {
	return nil, errors.New("term: PTY/login shell is only implemented on darwin")
}

func (p *PtyShell) Read([]byte) (int, error)  { return 0, io.EOF }
func (p *PtyShell) Write([]byte) (int, error) { return 0, io.EOF }
func (p *PtyShell) Resize(cols, rows int) error {
	_ = cols
	_ = rows
	return io.EOF
}
func (p *PtyShell) Close() error { return nil }
func (p *PtyShell) Wait() error  { return nil }
