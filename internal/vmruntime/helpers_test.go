package vmruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestSerialTranscriptSpillsAndStreamsIncrementally(t *testing.T) {
	transcript := NewSerialTranscript()
	data := bytes.Repeat([]byte("serial-output\n"), (2<<20)/len("serial-output\n"))
	if n, err := transcript.Write(data); err != nil || n != len(data) {
		t.Fatalf("write = %d, %v", n, err)
	}
	transcript.mu.Lock()
	path := transcript.path
	spilled := transcript.file != nil
	inMemory := len(transcript.buf)
	transcript.mu.Unlock()
	if !spilled || inMemory != 0 {
		t.Fatalf("spilled = %t, retained memory = %d", spilled, inMemory)
	}
	var got []byte
	for offset := 0; offset < transcript.Len(); {
		chunk, next := transcript.ReadFrom(offset)
		if next <= offset || len(chunk) > serialTranscriptReadBytes {
			t.Fatalf("read offset %d returned %d bytes and next %d", offset, len(chunk), next)
		}
		got = append(got, chunk...)
		offset = next
	}
	if !bytes.Equal(got, data) {
		t.Fatal("incremental transcript differed from written data")
	}
	if err := transcript.Close(); err != nil {
		t.Fatal(err)
	}
	if path != "" {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("spill remained after close: %v", err)
		}
	}
}

func TestSerialTranscriptCloseWakesPollingReaderWithoutPanicking(t *testing.T) {
	transcript := NewSerialTranscript()
	if _, err := transcript.Write([]byte("prefix")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := transcript.WaitFor(context.Background(), transcript.Len(), func(string) bool { return false })
		done <- err
	}()
	if err := transcript.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, os.ErrClosed) {
		t.Fatalf("wait after close = %v", err)
	}
}
