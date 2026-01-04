package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tinyrange/cc/internal/timeslice"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	filename := fs.String("filename", "", "Timeslice file to read")
	sums := fs.Bool("sums", false, "Print sums of timeslice durations")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if *filename == "" {
		fs.Usage()
		os.Exit(1)
	}

	f, err := os.Open(*filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open timeslice file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if *sums {
		sums := map[string]time.Duration{}
		if err := timeslice.ReadAllRecords(f, func(id string, flags timeslice.SliceFlags, duration time.Duration) error {
			sums[id] += duration
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to read timeslice file: %v\n", err)
			os.Exit(1)
		}
		for id, sum := range sums {
			fmt.Printf("%s %s\n", id, sum)
		}
	} else {
		if err := timeslice.ReadAllRecords(f, func(id string, flags timeslice.SliceFlags, duration time.Duration) error {
			fmt.Printf("%s %s %s\n", id, flags, duration)
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to read timeslice file: %v\n", err)
			os.Exit(1)
		}
	}
}
