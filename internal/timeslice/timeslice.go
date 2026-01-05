package timeslice

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

const (
	Magic   uint32 = 0x54534c46 // "TSLF"
	Version uint32 = 2
)

type header struct {
	Magic             uint32
	Version           uint32
	RecordKindsLength uint32
}

type TimesliceID uint64

const InvalidTimesliceID = TimesliceID(0)

var TimesliceInit = RegisterKind("init", SliceFlagInitTime)

type SliceInfo struct {
	Name  string
	Flags SliceFlags
}

type SliceFlags uint32

func (f SliceFlags) String() string {
	flags := []string{}
	if f&SliceFlagGuestTime != 0 {
		flags = append(flags, "guest")
	}
	if f&SliceFlagInitTime != 0 {
		flags = append(flags, "init")
	}
	return strings.Join(flags, ",")
}

const (
	SliceFlagGuestTime SliceFlags = 1 << iota
	SliceFlagInitTime
)

var timeslices = make(map[TimesliceID]SliceInfo)

// not designed to be thread safe
func RegisterKind(name string, flags SliceFlags) TimesliceID {
	id := TimesliceID(len(timeslices) + 1)
	timeslices[id] = SliceInfo{
		Name:  name,
		Flags: flags,
	}
	return id
}

type record struct {
	ID       TimesliceID
	Duration int64
}

var recordSize = binary.Size(record{})

type writer struct {
	w                   io.Writer
	writeThreadComplete chan error
	writerChan          chan record
}

func (w *writer) run() {
	defer close(w.writeThreadComplete)

	var buf [4096]byte
	off := 0

	// write records to the buffer flushing to the writer when the buffer is full
	for record := range w.writerChan {
		if off+recordSize > len(buf) {
			if _, err := w.w.Write(buf[:off]); err != nil {
				w.writeThreadComplete <- err
				return
			}
			off = 0
		}
		binary.LittleEndian.PutUint64(buf[off:off+8], uint64(record.ID))
		binary.LittleEndian.PutUint64(buf[off+8:off+16], uint64(record.Duration))
		off += recordSize
	}

	// flush any remaining data
	if off > 0 {
		if _, err := w.w.Write(buf[:off]); err != nil {
			w.writeThreadComplete <- err
			return
		}
	}

	// signal that the write thread is complete
	w.writeThreadComplete <- nil
}

func (w *writer) Close() error {
	// check if we're already closed, this also guarantees that we are the thread closing
	if !currentWriter.CompareAndSwap(w, nil) {
		return fmt.Errorf("timeslice: already closed")
	}

	// close the writer channel to signal the write thread to stop
	close(w.writerChan)

	// wait for the write thread to complete
	if err := <-w.writeThreadComplete; err != nil {
		return fmt.Errorf("timeslice: write thread: %w", err)
	}

	return nil
}

var currentWriter atomic.Pointer[writer]

var lastTime atomic.Uint64

func init() {
	lastTime.Store(uint64(time.Now().UnixNano()))
}

// Recorder is a helper to record timeslices.
// It is not thread safe, and should not be used concurrently.
type Recorder struct {
	last time.Time
}

func (r *Recorder) Record(id TimesliceID) {
	duration := time.Since(r.last)
	r.last = time.Now()
	Record(id, duration)
}

func NewRecorder() *Recorder {
	return &Recorder{
		last: time.Now(),
	}
}

func Record(id TimesliceID, duration time.Duration) {
	if w := currentWriter.Load(); w != nil {
		w.writerChan <- record{
			ID:       id,
			Duration: duration.Nanoseconds(),
		}
	}
}

func Open(w io.Writer) (io.Closer, error) {
	// check if we already have a writer
	if w := currentWriter.Load(); w != nil {
		return nil, fmt.Errorf("timeslice: already open")
	}

	slices, err := json.Marshal(timeslices)
	if err != nil {
		return nil, fmt.Errorf("timeslice: marshal timeslices: %w", err)
	}

	off := 0

	if err := binary.Write(w, binary.LittleEndian, header{
		Magic:             Magic,
		Version:           Version,
		RecordKindsLength: uint32(len(slices)),
	}); err != nil {
		return nil, fmt.Errorf("timeslice: write header: %w", err)
	}

	off += binary.Size(header{})

	if _, err := w.Write(slices); err != nil {
		return nil, fmt.Errorf("timeslice: write slices: %w", err)
	}
	off += len(slices)

	// pad to 4096 so we're aligned
	if off%4096 != 0 {
		if _, err := w.Write(make([]byte, 4096-off%4096)); err != nil {
			return nil, fmt.Errorf("timeslice: write padding: %w", err)
		}
		off += 4096 - off%4096
	}

	writer := &writer{w: w,
		writerChan:          make(chan record, 4096),
		writeThreadComplete: make(chan error),
	}
	go writer.run()

	if !currentWriter.CompareAndSwap(nil, writer) {
		return nil, fmt.Errorf("timeslice: already open")
	}

	return writer, nil
}

func ReadAllRecords(r io.Reader, fn func(id string, flags SliceFlags, duration time.Duration) error) error {
	var timeslices map[TimesliceID]SliceInfo

	buf := bufio.NewReaderSize(r, 4096)

	// read the header
	var header header
	if err := binary.Read(buf, binary.LittleEndian, &header); err != nil {
		return err
	}
	if header.Magic != Magic {
		return fmt.Errorf("timeslice: invalid magic")
	}
	if header.Version != Version {
		return fmt.Errorf("timeslice: invalid version")
	}

	// decode the timeslices map
	dec := json.NewDecoder(io.LimitReader(buf, int64(header.RecordKindsLength)))
	if err := dec.Decode(&timeslices); err != nil {
		return err
	}

	// skip the padding
	off := int(header.RecordKindsLength) + binary.Size(header)
	if off%4096 != 0 {
		if _, err := buf.Discard(4096 - off%4096); err != nil {
			return err
		}
	}

	// read the records
	for {
		var record record
		if err := binary.Read(buf, binary.LittleEndian, &record); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		kind, ok := timeslices[record.ID]
		if !ok {
			return fmt.Errorf("timeslice: unknown kind: %d", record.ID)
		}
		if err := fn(kind.Name, kind.Flags, time.Duration(record.Duration)); err != nil {
			return err
		}
	}

	return nil
}
