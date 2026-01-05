package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/tinyrange/cc/internal/timeslice"
)

var (
	tsKind = timeslice.RegisterKind("tstest", 0)
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	count := fs.Int("count", 100, "Number of events to write")
	threads := fs.Int("threads", 1, "Number of threads to use")
	filename := fs.String("filename", "local/test.tsfile", "Filename to write the events to")

	fs.Parse(os.Args[1:])

	f, err := os.Create(*filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file %q: %v\n", *filename, err)
		os.Exit(1)
	}
	defer f.Close()

	w, err := timeslice.Open(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open timeslice file %q: %v\n", *filename, err)
		os.Exit(1)
	}
	defer w.Close()

	wg := sync.WaitGroup{}
	wg.Add(*threads)
	for i := 0; i < *threads; i++ {
		go func() {
			defer wg.Done()

			for i := 0; i < *count; i++ {
				timeslice.Record(tsKind)
			}
		}()
	}
	wg.Wait()
}
