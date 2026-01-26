// Command runtest executes YAML-driven tests for example services.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/tinyrange/cc/examples/shared/testrunner"
)

func main() {
	verbose := flag.Bool("v", false, "Verbose output")
	keepAlive := flag.Bool("keep-alive", false, "Keep servers running after tests")
	parallel := flag.Int("p", 4, "Number of parallel builds")
	flag.Parse()

	runner := testrunner.NewRunner()
	runner.Verbose = *verbose
	runner.KeepAlive = *keepAlive
	runner.Parallel = *parallel

	// Get patterns from args
	patterns := flag.Args()

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cleaning up...")
		cancel()
	}()

	// Run tests
	results, err := runner.Run(ctx, patterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	testrunner.PrintResults(results)

	// Exit with error code if tests failed
	if results.Failed > 0 {
		os.Exit(1)
	}
}
