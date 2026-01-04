package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tinyrange/cc/internal/timeslice"
)

type timesliceRecord struct {
	ID    string
	Flags timeslice.SliceFlags
	Count int
	Sum   time.Duration
	Min   time.Duration
	Max   time.Duration
}

func (r *timesliceRecord) String() string {
	return fmt.Sprintf("% 40s flags=% 10s count=% 8d sum=% 16s min=% 16s max=% 16s avg=% 16s",
		r.ID, r.Flags, r.Count,
		r.Sum,
		r.Min,
		r.Max,
		r.Sum/time.Duration(r.Count),
	)
}

func (r *timesliceRecord) Add(duration time.Duration) {
	r.Count++
	r.Sum += duration
	if r.Min == 0 || duration < r.Min {
		r.Min = duration
	}
	if r.Max == 0 || duration > r.Max {
		r.Max = duration
	}
}

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
		records := map[string]*timesliceRecord{}
		displayOrder := []string{}
		if err := timeslice.ReadAllRecords(f, func(id string, flags timeslice.SliceFlags, duration time.Duration) error {
			record, ok := records[id]
			if !ok {
				displayOrder = append(displayOrder, id)
				record = &timesliceRecord{ID: id, Flags: flags}
				records[id] = record
			}
			record.Add(duration)
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to read timeslice file: %v\n", err)
			os.Exit(1)
		}
		for _, id := range displayOrder {
			record := records[id]
			fmt.Printf("%s\n", record.String())
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
