package helper

import (
	"flag"
	"fmt"
	"os"

	"github.com/tinyrange/cc/internal/ipc"
)

// Main runs the cc-helper process.
func Main() {
	socketPath := flag.String("socket", "", "Unix socket path to listen on")
	flag.Parse()

	if *socketPath == "" {
		fmt.Fprintln(os.Stderr, "cc-helper: -socket is required")
		os.Exit(1)
	}

	// Create helper state
	helper := NewHelper()
	defer helper.Close()

	// Create mux and register handlers
	mux := ipc.NewMux()
	helper.RegisterHandlers(mux)

	// Create server with mux (enables streaming handler support)
	server, err := ipc.NewServerWithMux(*socketPath, mux)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc-helper: failed to create server: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signalNotify(sigCh)
	go func() {
		<-sigCh
		server.Close()
	}()

	// Serve one connection (one helper per instance)
	if err := server.ServeOne(); err != nil {
		fmt.Fprintf(os.Stderr, "cc-helper: serve error: %v\n", err)
		os.Exit(1)
	}
}
