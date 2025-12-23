package debug

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"sync/atomic"
	"time"
)

// Debug is a thread-safe binary logger that writes to a file.

// Each log line contains a timestamp, source, and message.
// The binary format is:
//   - 2 bytes type (0 = invalid, 1 = bytes, 2 = string)
//   - 2 bytes source length
//   - 4 bytes message length
//   - 8 bytes timestamp (nanoseconds since epoch)
//   - sourceLength bytes source
//   - messageLength bytes message

// The way thread-safety is achieved is by atomically adding to the current offset of the file.

type Writer interface {
	io.WriterAt
	io.Closer
}

type writer struct {
	w Writer
}

var (
	fh     atomic.Pointer[writer]
	offset atomic.Uint64
)

func OpenFile(filename string) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return Open(f)
}

// The error is a warning, not an error. It indicates possible data loss.
func Open(w Writer) error {
	offset.Store(0)
	if fh.Swap(&writer{w: w}) != nil {
		return fmt.Errorf("debug: already open, discarded old writer")
	}
	return nil
}

func Close() error {
	fh := fh.Swap(nil)
	if fh != nil {
		if err := fh.w.Close(); err != nil {
			return err
		}
	}
	offset.Store(0)
	return nil
}

type DebugKind uint16

const (
	DebugKindInvalid DebugKind = iota
	DebugKindBytes
	DebugKindString
)

func encodeHeader(kind DebugKind, source string, data []byte) ([]byte, int64) {
	header := make([]byte, 16)
	binary.LittleEndian.PutUint16(header[0:2], uint16(kind))
	binary.LittleEndian.PutUint16(header[2:4], uint16(len(source)))
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(data)))
	binary.LittleEndian.PutUint64(header[8:16], uint64(time.Now().UnixNano()))
	return header, int64(len(source) + len(data) + 16)
}

func decodeHeader(header [16]byte) (kind DebugKind, sourceLength uint16, dataLength uint32) {
	kind = DebugKind(binary.LittleEndian.Uint16(header[0:2]))
	sourceLength = binary.LittleEndian.Uint16(header[2:4])
	dataLength = binary.LittleEndian.Uint32(header[4:8])
	return
}

func decodeTimestamp(header [16]byte) time.Time {
	return time.Unix(0, int64(binary.LittleEndian.Uint64(header[8:16])))
}

func writeBytes(kind DebugKind, source string, data []byte) {
	fh := fh.Load()
	if fh == nil {
		return
	}

	header, size := encodeHeader(kind, source, data)
	off := offset.Add(uint64(size)) - uint64(size)
	if _, err := fh.w.WriteAt(header, int64(off)); err != nil {
		panic(err)
	}
	// write source after the header
	if _, err := fh.w.WriteAt([]byte(source), int64(off)+16); err != nil {
		panic(err)
	}
	// write data after the source
	if _, err := fh.w.WriteAt(data, int64(off)+16+int64(len(source))); err != nil {
		panic(err)
	}
}

func WriteBytes(source string, data []byte) {
	writeBytes(DebugKindBytes, source, data)
}

func Write(source string, data string) {
	writeBytes(DebugKindString, source, []byte(data))
}

func Writef(source string, format string, args ...any) {
	writeBytes(DebugKindString, source, fmt.Appendf(nil, format, args...))
}

type Debug interface {
	WriteBytes(data []byte)
	Write(data string)
	Writef(format string, args ...any)
}

type debugImpl struct {
	source string
}

func (d *debugImpl) WriteBytes(data []byte) {
	writeBytes(DebugKindBytes, d.source, data)
}

func (d *debugImpl) Write(data string) {
	writeBytes(DebugKindString, d.source, []byte(data))
}

func (d *debugImpl) Writef(format string, args ...any) {
	writeBytes(DebugKindString, d.source, fmt.Appendf(nil, format, args...))
}

func WithSource(source string) Debug {
	return &debugImpl{source: source}
}

type SearchOptions struct {
	// The start and end timestamps to search within.
	Start time.Time
	End   time.Time

	// LimitStart only returns the first N entries after the start timestamp.
	// If both LimitStart and LimitEnd are set then an error is returned.
	LimitStart int64

	// LimitEnd only returns the last N entries before the end timestamp.
	// If both LimitStart and LimitEnd are set then an error is returned.
	LimitEnd int64

	// Only return entries for the given sources.
	Sources []string
}

type Reader interface {
	// Return a list of all sources in the order they were written.
	Sources() []string

	// Return the earliest and latest timestamps in the log.
	TimeRange() (time.Time, time.Time)

	// Guaranteed to iterate over all entries in the order they were written.
	Each(fn func(ts time.Time, kind DebugKind, source string, data []byte) error) error

	// Guaranteed to iterate over all entries for a given source in the order they were written.
	EachSource(source string, fn func(ts time.Time, kind DebugKind, data []byte) error) error

	// Guaranteed to iterate over all entries that match the search criteria in the order they were written.
	Search(opts SearchOptions, fn func(ts time.Time, kind DebugKind, source string, data []byte) error) error

	// Return the number of entries that match the search criteria.
	Count(opts SearchOptions) (int, error)
}

type indexEntry struct {
	offset int64
	ts     time.Time
}

type reader struct {
	r io.ReaderAt

	// maps source name to a list of index entries
	index map[string][]indexEntry

	// earliest and latest timestamps
	earliest time.Time
	latest   time.Time
}

func (r *reader) indexAll() error {
	var off int64
	var headerBytes [16]byte

	for {
		if _, err := r.r.ReadAt(headerBytes[:], off); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		kind, sourceLength, dataLength := decodeHeader(headerBytes)
		if kind == DebugKindInvalid {
			return fmt.Errorf("invalid header")
		}
		ts := decodeTimestamp(headerBytes)
		if ts.Before(r.earliest) {
			r.earliest = ts
		}
		if ts.After(r.latest) {
			r.latest = ts
		}
		// Read the source
		source := make([]byte, sourceLength)
		if _, err := r.r.ReadAt(source, off+16); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		r.index[string(source)] = append(r.index[string(source)], indexEntry{offset: off, ts: ts})

		off += int64(16 + int64(sourceLength) + int64(dataLength))
	}

	return nil
}

// Search implements Reader.
func (r *reader) Search(opts SearchOptions, fn func(ts time.Time, kind DebugKind, source string, data []byte) error) error {
	// Validate options
	if opts.LimitStart > 0 && opts.LimitEnd > 0 {
		return fmt.Errorf("cannot set both LimitStart and LimitEnd")
	}

	// Collect all matching entries with their source
	type sourceEntry struct {
		source string
		entry  indexEntry
	}
	var entries []sourceEntry

	// Build source filter set if sources are specified
	sourceFilter := make(map[string]struct{})
	for _, s := range opts.Sources {
		sourceFilter[s] = struct{}{}
	}

	for source, idxEntries := range r.index {
		// Filter by source if specified
		if len(sourceFilter) > 0 {
			if _, ok := sourceFilter[source]; !ok {
				continue
			}
		}

		for _, ie := range idxEntries {
			// Filter by time range
			if !opts.Start.IsZero() && ie.ts.Before(opts.Start) {
				continue
			}
			if !opts.End.IsZero() && ie.ts.After(opts.End) {
				continue
			}
			entries = append(entries, sourceEntry{source: source, entry: ie})
		}
	}

	// Sort by timestamp
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].entry.ts.Before(entries[j].entry.ts)
	})

	// Apply limits
	if opts.LimitStart > 0 && int64(len(entries)) > opts.LimitStart {
		entries = entries[:opts.LimitStart]
	}
	if opts.LimitEnd > 0 && int64(len(entries)) > opts.LimitEnd {
		entries = entries[len(entries)-int(opts.LimitEnd):]
	}

	// Iterate in sorted order
	for _, e := range entries {
		var headerBytes [16]byte
		if _, err := r.r.ReadAt(headerBytes[:], e.entry.offset); err != nil {
			return err
		}

		kind, sourceLength, dataLength := decodeHeader(headerBytes)
		if kind == DebugKindInvalid {
			return fmt.Errorf("invalid header")
		}

		data := make([]byte, dataLength)
		if _, err := r.r.ReadAt(data, e.entry.offset+16+int64(sourceLength)); err != nil {
			return err
		}
		if err := fn(e.entry.ts, kind, e.source, data); err != nil {
			return err
		}
	}
	return nil
}

// Count implements Reader.
func (r *reader) Count(opts SearchOptions) (int, error) {
	// Validate options
	if opts.LimitStart > 0 && opts.LimitEnd > 0 {
		return 0, fmt.Errorf("cannot set both LimitStart and LimitEnd")
	}

	// Build source filter set if sources are specified
	sourceFilter := make(map[string]struct{})
	for _, s := range opts.Sources {
		sourceFilter[s] = struct{}{}
	}

	count := 0
	for source, idxEntries := range r.index {
		// Filter by source if specified
		if len(sourceFilter) > 0 {
			if _, ok := sourceFilter[source]; !ok {
				continue
			}
		}

		for _, ie := range idxEntries {
			// Filter by time range
			if !opts.Start.IsZero() && ie.ts.Before(opts.Start) {
				continue
			}
			if !opts.End.IsZero() && ie.ts.After(opts.End) {
				continue
			}
			count++
		}
	}

	// Apply limits
	if opts.LimitStart > 0 && int64(count) > opts.LimitStart {
		count = int(opts.LimitStart)
	}
	if opts.LimitEnd > 0 && int64(count) > opts.LimitEnd {
		count = int(opts.LimitEnd)
	}

	return count, nil
}

// Each implements Reader.
func (r *reader) Each(fn func(ts time.Time, kind DebugKind, source string, data []byte) error) error {
	return r.Search(SearchOptions{}, fn)
}

// EachSource implements Reader.
func (r *reader) EachSource(source string, fn func(ts time.Time, kind DebugKind, data []byte) error) error {
	return r.Search(SearchOptions{Sources: []string{source}}, func(ts time.Time, kind DebugKind, _ string, data []byte) error {
		return fn(ts, kind, data)
	})
}

func (r *reader) Sources() []string {
	sources := make([]string, 0, len(r.index))
	for source := range r.index {
		sources = append(sources, source)
	}
	return sources
}

func (r *reader) TimeRange() (time.Time, time.Time) {
	return r.earliest, r.latest
}

func NewReader(r io.ReaderAt) (Reader, error) {
	ret := &reader{
		r:        r,
		index:    make(map[string][]indexEntry),
		earliest: time.Unix(0, 0),
		latest:   time.Unix(0, 0),
	}

	if err := ret.indexAll(); err != nil {
		return nil, err
	}

	return ret, nil
}

func NewReaderFromFile(filename string) (Reader, io.Closer, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	reader, err := NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return reader, f, nil
}
