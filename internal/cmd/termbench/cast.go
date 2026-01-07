package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// CastHeader represents the header of an asciinema cast file.
type CastHeader struct {
	Version   int               `json:"version"`
	Term      CastTerm          `json:"term"`
	Width     int               `json:"width,omitempty"`  // v2 format
	Height    int               `json:"height,omitempty"` // v2 format
	Timestamp int64             `json:"timestamp,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// Cols returns the terminal column count (handles both v2 and v3 formats).
func (h *CastHeader) Cols() int {
	if h.Term.Cols > 0 {
		return h.Term.Cols
	}
	return h.Width
}

// Rows returns the terminal row count (handles both v2 and v3 formats).
func (h *CastHeader) Rows() int {
	if h.Term.Rows > 0 {
		return h.Term.Rows
	}
	return h.Height
}

// CastTerm represents terminal information in the cast header (v3 format).
type CastTerm struct {
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Type  string `json:"type,omitempty"`
	Theme *struct {
		Fg      string `json:"fg,omitempty"`
		Bg      string `json:"bg,omitempty"`
		Palette string `json:"palette,omitempty"`
	} `json:"theme,omitempty"`
}

// CastEvent represents a single event in a cast file.
type CastEvent struct {
	Time float64
	Type string
	Data string
}

// UnmarshalJSON implements custom JSON unmarshaling for CastEvent.
// Events are stored as arrays: [time, type, data]
func (e *CastEvent) UnmarshalJSON(data []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) < 3 {
		return fmt.Errorf("event array too short: %d elements", len(arr))
	}

	if err := json.Unmarshal(arr[0], &e.Time); err != nil {
		return fmt.Errorf("parse event time: %w", err)
	}
	if err := json.Unmarshal(arr[1], &e.Type); err != nil {
		return fmt.Errorf("parse event type: %w", err)
	}
	if err := json.Unmarshal(arr[2], &e.Data); err != nil {
		return fmt.Errorf("parse event data: %w", err)
	}

	return nil
}

// ParseCast reads and parses an asciinema cast file.
func ParseCast(path string) (*CastHeader, []CastEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open cast file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for long lines (cast files can have very long data strings).
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	// First line is the header.
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, nil, fmt.Errorf("read header: %w", err)
		}
		return nil, nil, fmt.Errorf("empty cast file")
	}

	var header CastHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return nil, nil, fmt.Errorf("parse header: %w", err)
	}

	// Remaining lines are events.
	// Event times are deltas from the previous event, so we accumulate them
	// into absolute times from the start.
	var events []CastEvent
	var absoluteTime float64
	lineNum := 1
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event CastEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, nil, fmt.Errorf("parse event at line %d: %w", lineNum, err)
		}
		// Convert delta to absolute time.
		absoluteTime += event.Time
		event.Time = absoluteTime
		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read cast file: %w", err)
	}

	return &header, events, nil
}
