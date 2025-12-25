package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime/pprof"
	"time"

	"github.com/tinyrange/cc/internal/debug"
)

func getModTime(filename string) (time.Time, error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

func run() error {
	// Flags
	list := flag.Bool("list", false, "list all sources in the log")
	sample := flag.Bool("sample", false, "print one record from each matched source")
	timeRange := flag.Bool("range", false, "print the earliest and latest timestamps")
	source := flag.String("source", "", "regex to filter sources")
	match := flag.String("match", "", "regex to filter messages")
	limit := flag.Int("limit", 100, "limit the number of entries (0 for unlimited)")
	tail := flag.Bool("tail", false, "show last N entries instead of first N")
	cpuprofile := flag.String("cpuprofile", "", "write CPU profile to file")
	memprofile := flag.String("memprofile", "", "write memory profile to file")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `debug - inspect binary debug logs

USAGE:
  debug [flags] <filename>

FLAGS:
  -list          List all unique source names in the log, one per line
  -sample	     Print one record from each matched source
  -range         Show earliest/latest timestamps and total duration
  -source REGEX  Only show entries where source matches regex (Go regexp syntax)
  -match REGEX   Only show entries where message matches regex (Go regexp syntax)
  -limit N       Max entries to return (default: 100). Errors if exceeded; use -tail or 0 for unlimited
  -tail          Show last N entries instead of first N (combine with -limit)

OUTPUT FORMAT:
  Each entry is printed as: TIMESTAMP [SOURCE] MESSAGE
  Timestamps are RFC3339Nano format (e.g. 2024-01-15T10:30:00.123456789Z)

EXAMPLES:
  debug log.bin                          Show entries (errors if >100)
  debug -sample log.bin                  Print one record from each matched source
  debug -tail log.bin                    Show last 100 entries
  debug -limit 0 log.bin                 Show all entries (no limit)
  debug -tail -limit 50 log.bin          Show last 50 entries
  debug -list log.bin                    List all source names
  debug -range log.bin                   Show time range of log
  debug -source '^tcp' log.bin           Entries from sources starting with "tcp"
  debug -source 'tcp|udp' log.bin        Entries from tcp or udp sources
  debug -match 'error' log.bin           Entries containing "error" in message
  debug -match '(?i)error' log.bin       Case-insensitive match for "error"
  debug -source 'net' -match 'timeout' log.bin   Combine source and message filters
`)
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("create CPU profile file: %w", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			return fmt.Errorf("create memory profile file: %w", err)
		}
		defer f.Close()
		if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
			return fmt.Errorf("write memory profile: %w", err)
		}
	}

	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}

	filename := flag.Arg(0)

	reader, closer, err := debug.NewReaderFromFile(filename)
	if err != nil {
		return fmt.Errorf("failed to open debug file: %w", err)
	}
	defer closer.Close()

	// Handle -list
	if *list {
		for _, src := range reader.Sources() {
			fmt.Println(src)
		}
		return nil
	}

	// Handle -range
	if *timeRange {
		earliest, latest := reader.TimeRange()
		fmt.Printf("earliest: %s\nlatest:   %s\nduration: %s\n", earliest, latest, latest.Sub(earliest))
		return nil
	}

	// Handle -sample
	if *sample {
		if err := reader.Sample(func(ts time.Time, kind debug.DebugKind, src string, data []byte) error {
			fmt.Printf("%s [%s] %s\n", ts.Format(time.RFC3339Nano), src, string(data))
			return nil
		}); err != nil {
			return fmt.Errorf("failed to sample log: %w", err)
		}
		return nil
	}

	// Compile regexes
	var sourceRe, matchRe *regexp.Regexp
	if *source != "" {
		sourceRe, err = regexp.Compile(*source)
		if err != nil {
			return fmt.Errorf("invalid source regex: %w", err)
		}
	}
	if *match != "" {
		matchRe, err = regexp.Compile(*match)
		if err != nil {
			return fmt.Errorf("invalid match regex: %w", err)
		}
	}

	// Collect matching entries
	type entry struct {
		ts     time.Time
		source string
		data   []byte
	}
	var entries []entry

	if err := reader.Each(func(ts time.Time, kind debug.DebugKind, src string, data []byte) error {
		// Filter by source regex
		if sourceRe != nil && !sourceRe.MatchString(src) {
			return nil
		}
		// Filter by message regex
		if matchRe != nil && !matchRe.MatchString(string(data)) {
			return nil
		}
		entries = append(entries, entry{ts: ts, source: src, data: data})
		return nil
	}); err != nil {
		return fmt.Errorf("failed to read log: %w", err)
	}

	// Apply limit
	if *limit == 100 {
		if len(entries) > *limit {
			if *tail {
				// -tail explicitly requests last N, so truncate
				entries = entries[len(entries)-*limit:]
			} else {
				return fmt.Errorf("too many entries: %d (limit is %d). Use -tail for last %d, or explicitly set a limit using -limit", len(entries), *limit, *limit)
			}
		}
	} else if *limit > 0 {
		if len(entries) > *limit {
			if *tail {
				entries = entries[len(entries)-*limit:]
			} else {
				entries = entries[:*limit]
			}
		} else {
			if len(entries) > *limit {
				entries = entries[:*limit]
			}
		}
	}

	// Print entries
	for _, e := range entries {
		fmt.Printf("%s [%s] %s\n", e.ts.Format(time.RFC3339Nano), e.source, string(e.data))
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "debug: %v\n", err)
		os.Exit(1)
	}
}
