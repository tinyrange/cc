package debug

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"sync"
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

type write struct {
	off  int64
	data []byte
}

type logStructuredBuffer struct {
	data    sync.Map
	maxSize atomic.Int64
}

func (b *logStructuredBuffer) WriteAt(p []byte, off int64) (n int, err error) {
	b.data.Store(off, write{
		off:  off,
		data: append([]byte{}, p...),
	})
	val := b.maxSize.Load()
	if val < int64(len(p))+off {
		for {
			if b.maxSize.CompareAndSwap(val, int64(len(p))+off) {
				break
			}
			val = b.maxSize.Load()
		}
	}
	return len(p), nil
}

func (b *logStructuredBuffer) Close() error {
	return nil
}

type compiledBuffer []byte

func (b *compiledBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= int64(len(*b)) {
		return 0, io.EOF
	}
	return copy(p, (*b)[off:]), nil
}

func (b *logStructuredBuffer) Compile() (compiledBuffer, error) {
	data := make([]byte, b.maxSize.Load())
	b.data.Range(func(key, value any) bool {
		off := key.(int64)
		write := value.(write)
		copy(data[off:off+int64(len(write.data))], write.data)
		return true
	})

	return compiledBuffer(data), nil
}

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
	// Truncate to ensure successive runs don't leave stale trailing entries.
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

type WriterTo interface {
	WriteTo(w io.WriterAt) (n int64, err error)
}

type memoryWriter struct {
	logStructuredBuffer
}

func (m *memoryWriter) WriteTo(w io.WriterAt) (n int64, err error) {
	m.data.Range(func(key, value any) bool {
		off := key.(int64)
		write := value.(write)
		if _, err := w.WriteAt(write.data, off); err != nil {
			return false
		}
		return true
	})
	return int64(m.maxSize.Load()), nil
}

func OpenMemory() (WriterTo, error) {
	mem := &memoryWriter{}
	if err := Open(mem); err != nil {
		return nil, err
	}
	return mem, nil
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

func decodeTimestamp(header [16]byte) int64 {
	return int64(binary.LittleEndian.Uint64(header[8:16]))
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
	Offset   int64
	UnixNano int64
}

type reader struct {
	r io.ReaderAt

	// maps source name to a list of index entries
	index      map[uint64][]indexEntry
	sourceList map[uint64]string

	// earliest and latest timestamps
	earliest int64
	latest   int64

	hash hash.Hash64
}

func (r *reader) hashBytes(s []byte) uint64 {
	r.hash.Reset()
	r.hash.Write(s)
	return r.hash.Sum64()
}

func (r *reader) hashString(s string) uint64 {
	r.hash.Reset()
	r.hash.Write([]byte(s))
	return r.hash.Sum64()
}

func (r *reader) indexAll(reader io.ReadSeeker) error {
	var headerBytes [16]byte

	br := bufio.NewReaderSize(reader, 8*1024*1024)

	currentOffset, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("failed to seek to current offset: %w", err)
	}

	var sourceBuffer [64 * 1024]byte

	for {
		if _, err := io.ReadFull(br, headerBytes[:]); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read header: %w", err)
		}
		kind, sourceLength, dataLength := decodeHeader(headerBytes)
		if kind == DebugKindInvalid {
			return fmt.Errorf("invalid header")
		}
		ts := decodeTimestamp(headerBytes)
		if r.earliest == 0 || ts < r.earliest {
			r.earliest = ts
		}
		if r.latest == 0 || ts > r.latest {
			r.latest = ts
		}

		// Read the source completely first
		if int(sourceLength) > len(sourceBuffer) {
			return fmt.Errorf("source length %d is greater than buffer size %d", sourceLength, len(sourceBuffer))
		}
		if _, err := io.ReadFull(br, sourceBuffer[:sourceLength]); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read source: %w", err)
		}

		sourceHash := r.hashBytes(sourceBuffer[:sourceLength])
		if _, ok := r.sourceList[sourceHash]; !ok {
			r.sourceList[sourceHash] = string(sourceBuffer[:sourceLength])
		}

		if _, err := br.Discard(int(dataLength)); err != nil {
			return fmt.Errorf("failed to discard data: %w", err)
		}

		if _, ok := r.index[sourceHash]; !ok {
			r.index[sourceHash] = make([]indexEntry, 0, 1024)
		}

		r.index[sourceHash] = append(
			r.index[sourceHash],
			indexEntry{Offset: currentOffset, UnixNano: ts},
		)

		currentOffset += int64(16 + int64(sourceLength) + int64(dataLength))
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
	sourceFilter := make(map[uint64]struct{})
	for _, s := range opts.Sources {
		sourceFilter[r.hashString(s)] = struct{}{}
	}

	for source, idxEntries := range r.index {
		// Filter by source if specified
		if len(sourceFilter) > 0 {
			if _, ok := sourceFilter[source]; !ok {
				continue
			}
		}

		for _, ie := range idxEntries {
			ts := time.Unix(0, ie.UnixNano)

			// Filter by time range
			if !opts.Start.IsZero() && ts.Before(opts.Start) {
				continue
			}
			if !opts.End.IsZero() && ts.After(opts.End) {
				continue
			}
			entries = append(entries, sourceEntry{source: r.sourceList[source], entry: ie})
		}
	}

	// Sort by timestamp
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].entry.UnixNano < entries[j].entry.UnixNano
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
		if _, err := r.r.ReadAt(headerBytes[:], e.entry.Offset); err != nil {
			return err
		}

		kind, sourceLength, dataLength := decodeHeader(headerBytes)
		if kind == DebugKindInvalid {
			return fmt.Errorf("invalid header")
		}

		data := make([]byte, dataLength)
		if _, err := r.r.ReadAt(data, e.entry.Offset+16+int64(sourceLength)); err != nil {
			return err
		}
		if err := fn(time.Unix(0, e.entry.UnixNano), kind, e.source, data); err != nil {
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
	sourceFilter := make(map[uint64]struct{})
	for _, s := range opts.Sources {
		sourceFilter[r.hashString(s)] = struct{}{}
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
			if !opts.Start.IsZero() && time.Unix(0, ie.UnixNano).Before(opts.Start) {
				continue
			}
			if !opts.End.IsZero() && time.Unix(0, ie.UnixNano).After(opts.End) {
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
	for _, source := range r.sourceList {
		sources = append(sources, source)
	}
	return sources
}

func (r *reader) TimeRange() (time.Time, time.Time) {
	return time.Unix(0, r.earliest), time.Unix(0, r.latest)
}

func NewReader(r io.ReaderAt, indexReader io.ReadSeeker) (Reader, error) {
	ret := &reader{
		r:          r,
		index:      make(map[uint64][]indexEntry),
		sourceList: make(map[uint64]string),
		hash:       fnv.New64a(),
	}

	if err := ret.indexAll(indexReader); err != nil {
		return nil, fmt.Errorf("failed to index file: %w", err)
	}

	return ret, nil
}

func NewReaderFromFile(filename string) (Reader, io.Closer, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	reader, err := NewReader(f, f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return reader, f, nil
}
