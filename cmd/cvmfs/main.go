package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	intcvmfs "j5.nz/cc/internal/cvmfs"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cvmfs:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("cvmfs", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	args = fs.Args()
	if len(args) < 2 {
		return fmt.Errorf("usage: cvmfs <ls|cat> <target>")
	}
	client := intcvmfs.NewClient()
	switch args[0] {
	case "ls":
		entries, err := client.ReadDir(args[1])
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name
			if entry.Mode.IsDir() {
				name += "/"
			}
			if _, err := fmt.Fprintln(stdout, name); err != nil {
				return err
			}
		}
		return nil
	case "cat":
		data, err := client.ReadFile(args[1])
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
