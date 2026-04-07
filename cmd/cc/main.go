package main

import (
	"encoding/json"
	"flag"
	"net"
	"os"
	"os/exec"

	"j5.nz/cc/client"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	ccvmPath := fs.String("ccvm", "", "Path to ccvm binary (d)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if *ccvmPath == "" {
		// Look next to the current executable for the ccvm binary.
		exePath, err := os.Executable()
		if err != nil {
			panic(err)
		}

		*ccvmPath = exePath + "vm"

		if _, err := os.Stat(*ccvmPath); os.IsNotExist(err) {
			panic("ccvm binary not found at " + *ccvmPath)
		}
	}

	proc := exec.Command(*ccvmPath)

	proc.Stderr = os.Stderr

	stdout, err := proc.StdoutPipe()
	if err != nil {
		panic(err)
	}

	if err := proc.Start(); err != nil {
		panic(err)
	}

	var hello client.ServerHello

	if err := json.NewDecoder(stdout).Decode(&hello); err != nil {
		panic(err)
	}

	client, err := client.NewClient("http://"+hello.Addr, func() (net.Conn, error) {
		return net.Dial("tcp", hello.Addr)
	}), nil
	if err != nil {
		panic(err)
	}

	if err := client.HealthCheck(); err != nil {
		panic(err)
	}

	if err := client.Shutdown(); err != nil {
		panic(err)
	}

	if err := proc.Wait(); err != nil {
		panic(err)
	}
}
