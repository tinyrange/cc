package session

import (
	"context"
	"strings"
	"time"

	"j5.nz/cc/client"
	"j5.nz/cc/internal/vmruntime"
)

type Transcript interface {
	String() string
}

type StreamExecOptions struct {
	Transcript     Transcript
	Start          int
	ID             string
	OnEvent        func(client.ExecEvent) error
	OnCallbackFail func()
	OnContextDone  func()
	OnObserve      func(StreamExecObservation)
	Wait           func(context.Context) error
}

type StreamExecObservation struct {
	Kind     string
	Line     string
	Duration time.Duration
	Matched  bool
	Done     bool
	Stats    StreamExecStats
}

type StreamExecStats struct {
	Loops   int
	Reads   int
	Lines   int
	Matched int
	Ignored int
	Waits   int
}

func StreamExecEvents(ctx context.Context, opts StreamExecOptions) error {
	offset := opts.Start
	var pending string
	var stats StreamExecStats
	totalStart := time.Now()
	observe := func(obs StreamExecObservation) {
		if opts.OnObserve != nil {
			opts.OnObserve(obs)
		}
	}
	for {
		stats.Loops++
		observe(StreamExecObservation{Kind: "loop", Stats: stats})
		text := ""
		readStart := time.Now()
		if opts.Transcript != nil {
			text = opts.Transcript.String()
		}
		observe(StreamExecObservation{Kind: "transcript_string", Duration: time.Since(readStart), Stats: stats})
		if offset < len(text) {
			stats.Reads++
			appendStart := time.Now()
			pending += text[offset:]
			offset = len(text)
			observe(StreamExecObservation{Kind: "append_pending", Duration: time.Since(appendStart), Stats: stats})
			for {
				lineStart := time.Now()
				lineEnd := strings.IndexByte(pending, '\n')
				if lineEnd < 0 {
					break
				}
				line := strings.TrimSpace(pending[:lineEnd])
				pending = pending[lineEnd+1:]
				stats.Lines++
				observe(StreamExecObservation{Kind: "line", Line: line, Duration: time.Since(lineStart), Stats: stats})
				parseStart := time.Now()
				event, done, ok, err := vmruntime.ParseManagedExecEventLine(line, opts.ID)
				parseDuration := time.Since(parseStart)
				if err != nil {
					return err
				}
				if !ok {
					stats.Ignored++
					observe(StreamExecObservation{Kind: "parse", Line: line, Duration: parseDuration, Matched: false, Done: done, Stats: stats})
					continue
				}
				stats.Matched++
				observe(StreamExecObservation{Kind: "parse", Line: line, Duration: parseDuration, Matched: true, Done: done, Stats: stats})
				if opts.OnEvent != nil {
					callbackStart := time.Now()
					if err := opts.OnEvent(event); err != nil {
						observe(StreamExecObservation{Kind: "callback", Duration: time.Since(callbackStart), Matched: true, Done: done, Stats: stats})
						if opts.OnCallbackFail != nil {
							opts.OnCallbackFail()
						}
						return err
					}
					observe(StreamExecObservation{Kind: "callback", Duration: time.Since(callbackStart), Matched: true, Done: done, Stats: stats})
				}
				if done {
					observe(StreamExecObservation{Kind: "done", Duration: time.Since(totalStart), Matched: true, Done: true, Stats: stats})
					return nil
				}
			}
			continue
		}
		if ctx.Err() != nil {
			if opts.OnContextDone != nil {
				opts.OnContextDone()
			}
			return ctx.Err()
		}
		if opts.Wait != nil {
			stats.Waits++
			waitStart := time.Now()
			if err := opts.Wait(ctx); err != nil {
				observe(StreamExecObservation{Kind: "wait", Duration: time.Since(waitStart), Stats: stats})
				return err
			}
			observe(StreamExecObservation{Kind: "wait", Duration: time.Since(waitStart), Stats: stats})
		}
	}
}
