package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/timeslice"
)

// FrameStats tracks frame timing statistics.
type FrameStats struct {
	times []time.Duration
}

// Add records a frame time.
func (s *FrameStats) Add(d time.Duration) {
	s.times = append(s.times, d)
}

// Count returns the number of recorded frames.
func (s *FrameStats) Count() int {
	return len(s.times)
}

// Min returns the minimum frame time.
func (s *FrameStats) Min() time.Duration {
	if len(s.times) == 0 {
		return 0
	}
	min := s.times[0]
	for _, d := range s.times[1:] {
		if d < min {
			min = d
		}
	}
	return min
}

// Max returns the maximum frame time.
func (s *FrameStats) Max() time.Duration {
	if len(s.times) == 0 {
		return 0
	}
	max := s.times[0]
	for _, d := range s.times[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

// Avg returns the average frame time.
func (s *FrameStats) Avg() time.Duration {
	if len(s.times) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range s.times {
		total += d
	}
	return total / time.Duration(len(s.times))
}

// Percentile returns the p-th percentile frame time (p in 0-100).
func (s *FrameStats) Percentile(p float64) time.Duration {
	if len(s.times) == 0 {
		return 0
	}
	// Make a sorted copy.
	sorted := make([]time.Duration, len(s.times))
	copy(sorted, s.times)
	slices.Sort(sorted)

	idx := max(int(float64(len(sorted)-1)*p/100.0), 0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// String returns a formatted summary of the stats.
func (s *FrameStats) String() string {
	return fmt.Sprintf("min=%v max=%v avg=%v p90=%v p99=%v",
		s.Min().Round(time.Microsecond),
		s.Max().Round(time.Microsecond),
		s.Avg().Round(time.Microsecond),
		s.Percentile(90).Round(time.Microsecond),
		s.Percentile(99).Round(time.Microsecond))
}

var (
	tsFrameStart = timeslice.RegisterKind("frame_start", 0)
	tsViewStep   = timeslice.RegisterKind("view_step", 0)
	tsEventFeed  = timeslice.RegisterKind("event_feed", 0)
	tsFrameEnd   = timeslice.RegisterKind("frame_end", 0)
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fast := flag.Bool("fast", false, "feed data as fast as possible (ignore timestamps)")
	cpuprofile := flag.String("cpuprofile", "", "write CPU profile to file")
	timesliceFile := flag.String("timeslice", "", "write timeslice to file")
	frames := flag.Int("frames", 0, "number of frames to replay (0 for all)")

	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating CPU profile file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "error starting CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
	}

	if *timesliceFile != "" {
		f, err := os.Create(*timesliceFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating timeslice file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()

		closeFunc, err := timeslice.StartRecording(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error starting timeslice: %v\n", err)
			os.Exit(1)
		}
		defer closeFunc.Close()
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: termbench [--fast] <cast-file>")
		os.Exit(1)
	}
	castPath := args[0]

	// Parse the cast file.
	header, events, err := ParseCast(castPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing cast file: %v\n", err)
		os.Exit(1)
	}

	cols := header.Cols()
	rows := header.Rows()
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}

	if *frames > 0 {
		events = events[:min(*frames, len(events))]
	}

	// Get total duration from last event time.
	var duration float64
	if len(events) > 0 {
		duration = events[len(events)-1].Time
	}

	fmt.Printf("Cast: %dx%d terminal, %d events, %.1fs duration\n", cols, rows, len(events), duration)

	// Calculate window size based on terminal dimensions.
	// Approximate cell size: 9px wide, 18px high at font size 16.
	const cellW, cellH = 9, 18
	const statsBarHeight = 32
	const padding = 20

	winWidth := cols*cellW + padding*2
	winHeight := rows*cellH + padding*2 + statsBarHeight

	// Create window.
	win, err := graphics.New("termbench", winWidth, winHeight)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating window: %v\n", err)
		os.Exit(1)
	}

	// Create terminal view.
	view, err := term.NewView(win)
	if err != nil {
		win.PlatformWindow().Close()
		fmt.Fprintf(os.Stderr, "error creating terminal view: %v\n", err)
		os.Exit(1)
	}
	defer view.Close()

	// Reserve top space for stats bar.
	view.SetInsets(0, statsBarHeight, 0, 0)

	// Create text renderer for stats overlay.
	txt, err := text.Load(win)
	if err != nil {
		win.PlatformWindow().Close()
		fmt.Fprintf(os.Stderr, "error creating text renderer: %v\n", err)
		os.Exit(1)
	}

	// Playback state.
	eventIdx := 0
	var startTime time.Time // Set on first event feed
	var stats FrameStats
	done := false

	// Track whether we've done the initial resize.
	firstFrame := true
	startTimeSet := false

	rec := timeslice.NewState()

	// Run the window loop.
	err = win.Loop(func(f graphics.Frame) error {
		rec.Record(tsFrameStart)

		frameStart := time.Now()

		// Render terminal view FIRST to handle resize before feeding events.
		width, height := f.WindowSize()
		txt.SetViewport(int32(width), int32(height))

		if err := view.Step(f, term.Hooks{}); err != nil {
			return err
		}

		rec.Record(tsViewStep)

		// Skip event feeding on first frame to allow resize to complete.
		if firstFrame {
			firstFrame = false
			return nil
		}

		// Feed events to terminal.
		if !done && eventIdx < len(events) {
			// Set startTime on first event feed.
			if !startTimeSet {
				startTime = time.Now()
				startTimeSet = true
			}

			if *fast {
				// Fast mode: feed one event per frame.
				ev := events[eventIdx]
				if ev.Type == "o" {
					_, _ = view.Write([]byte(ev.Data))
				}
				eventIdx++
			} else {
				// Real-time mode: feed events up to current time.
				elapsed := time.Since(startTime).Seconds()
				for eventIdx < len(events) {
					ev := events[eventIdx]
					if ev.Time > elapsed {
						break
					}
					if ev.Type == "o" {
						_, _ = view.Write([]byte(ev.Data))
					}
					eventIdx++
				}
			}
		}

		rec.Record(tsEventFeed)

		// Check if playback is complete.
		if eventIdx >= len(events) && !done {
			done = true
		}

		// Render stats bar background.
		f.RenderQuad(0, 0, float32(width), statsBarHeight, nil, color.RGBA{30, 30, 30, 255})

		// Render stats text.
		statsText := fmt.Sprintf("Frame: %s | Events: %d/%d",
			stats.String(), eventIdx, len(events))
		txt.RenderText(statsText, 10, statsBarHeight-8, 14, color.White)

		// Record frame time.
		frameTime := time.Since(frameStart)
		stats.Add(frameTime)

		rec.Record(tsFrameEnd)

		// Exit if playback is done.
		if done {
			return fmt.Errorf("playback complete")
		}

		return nil
	})

	// Print final stats.
	fmt.Println()
	fmt.Println("=== Final Statistics ===")
	fmt.Printf("Total frames: %d\n", stats.Count())
	fmt.Printf("Frame times: %s\n", stats.String())
	fmt.Printf("Events played: %d/%d\n", eventIdx, len(events))

	if err != nil && err.Error() != "playback complete" {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
