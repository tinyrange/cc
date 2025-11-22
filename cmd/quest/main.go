package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/tinyrange/cc/internal/asm/amd64"
	"github.com/tinyrange/cc/internal/ir"
	amd64ir "github.com/tinyrange/cc/internal/ir/amd64"
)

type bringupQuest struct{}

func (q *bringupQuest) Run() error {
	// try compiling a simple program for amd64 linux
	frag, err := amd64ir.Compile(ir.Method{
		ir.Printf("bringup-quest-ok\n"),
		ir.Return(ir.Int64(42)),
	})
	if err != nil {
		return fmt.Errorf("compile test program: %w", err)
	}

	if _, err := amd64.EmitStandaloneELF(frag); err != nil {
		return fmt.Errorf("emit ELF: %w", err)
	}

	slog.Info("Bringup Quest Completed")

	return nil
}

func main() {
	q := &bringupQuest{}

	if err := q.Run(); err != nil {
		slog.Error("failed bringup quest", "error", err)
		os.Exit(1)
	}
}
