package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/debug"
)

func run(args []string) error {
	usage := func() {
		fmt.Fprintf(os.Stderr, "usage: debug <filename> <command> [args...]\n")
		fmt.Fprintf(os.Stderr, "commands:\n")
		fmt.Fprintf(os.Stderr, "  list: list all sources in the log\n")
		fmt.Fprintf(os.Stderr, "  range: print the earliest and latest timestamps in the log\n")
		fmt.Fprintf(os.Stderr, "  search: search the log for entries matching the given criteria\n")
		os.Exit(1)
	}

	if len(args) < 1 {
		usage()
	}

	filename := args[0]

	reader, closer, err := debug.NewReaderFromFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open debug file: %v\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	if len(args) < 2 {
		usage()
	}

	switch args[1] {
	case "list":
		sources := reader.Sources()
		for _, source := range sources {
			fmt.Println(source)
		}
		return nil
	case "range":
		earliest, latest := reader.TimeRange()
		fmt.Printf("earliest: %s, latest: %s\n", earliest, latest)
		return nil
	case "search":
		fs := flag.NewFlagSet("debug", flag.ExitOnError)
		limit := fs.Int("limit", 0, "limit the number of entries to return (the default is 100 but 0 will error if there are more than 100 entries)")
		sources := fs.String("sources", "", "only return entries for the given sources (comma separated list)")
		start := fs.String("start", "", "only return entries after the given timestamp (RFC3339)")
		end := fs.String("end", "", "only return entries before the given timestamp (RFC3339)")
		tail := fs.Bool("tail", false, "only return the last N entries")
		if err := fs.Parse(args[2:]); err != nil {
			return fmt.Errorf("failed to parse flags: %w", err)
		}
		var opts debug.SearchOptions
		if *limit > 0 {
			if *tail {
				opts.LimitEnd = int64(*limit)
			} else {
				opts.LimitStart = int64(*limit)
			}
		} else {
			if *tail {
				opts.LimitEnd = 100
			} else {
				opts.LimitStart = 100
			}
		}
		if *sources != "" {
			opts.Sources = strings.Split(*sources, ",")
		}
		if *start != "" {
			opts.Start, err = time.Parse(time.RFC3339, *start)
			if err != nil {
				return fmt.Errorf("failed to parse start timestamp: %w", err)
			}
		}
		if *end != "" {
			opts.End, err = time.Parse(time.RFC3339, *end)
			if err != nil {
				return fmt.Errorf("failed to parse end timestamp: %w", err)
			}
		}
		if *limit == 0 {
			optsCopy := opts
			optsCopy.LimitStart = 0
			optsCopy.LimitEnd = 0

			count, err := reader.Count(optsCopy)
			if err != nil {
				return fmt.Errorf("failed to count entries: %w", err)
			}
			if count > int(max(opts.LimitStart, opts.LimitEnd)) {
				return fmt.Errorf("too many entries (got %d, limit is %d), use -limit to increase the limit", count, max(opts.LimitStart, opts.LimitEnd))
			}
		}
		if err := reader.Search(opts, func(ts time.Time, kind debug.DebugKind, source string, data []byte) error {
			fmt.Printf("%s: [%s] %s\n", ts, source, string(data))
			return nil
		}); err != nil {
			return fmt.Errorf("failed to search log: %w", err)
		}
		return nil
	default:
		usage()
		return nil
	}
}

func main() {
	// usage: debug <filename> <command> [args...]
	//
	// The command is one of:
	//   - list: list all sources in the log
	//   - range: print the earliest and latest timestamps in the log
	//   - each: print each entry in the log
	//   - each-source: print each entry for a given source in the log
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "debug: %v\n", err)
		os.Exit(1)
	}
}
