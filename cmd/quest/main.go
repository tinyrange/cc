package main

import (
	"fmt"
	"log/slog"
	"os"
)

type bringupQuest struct{}

func (q *bringupQuest) Run() error {
	return fmt.Errorf("not implemented")
}

func main() {
	q := &bringupQuest{}

	if err := q.Run(); err != nil {
		slog.Error("failed bringup quest", "error", err)
		os.Exit(1)
	}
}
