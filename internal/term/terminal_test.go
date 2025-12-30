package term

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func drainAllWithTimeout(t *testing.T, r io.Reader, timeout time.Duration) ([]byte, error) {
	t.Helper()

	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(r)
		ch <- result{b: b, err: err}
	}()

	select {
	case res := <-ch:
		return res.b, res.err
	case <-time.After(timeout):
		return nil, io.ErrNoProgress
	}
}

func TestDisableVTQueriesThatBreakGuests_SwallowsQueries(t *testing.T) {
	t.Run("DSR_CPR", func(t *testing.T) {
		emu := vt.NewSafeEmulator(80, 40)
		disableVTQueriesThatBreakGuests(emu)

		// Drain the emulator's input stream concurrently. The emulator writes reply
		// bytes to an io.Pipe, and writes will block until a reader is present.
		var got []byte
		var gotErr error
		done := make(chan struct{})
		go func() {
			got, gotErr = drainAllWithTimeout(t, emu, 2*time.Second)
			close(done)
		}()

		// DSR 6 = Cursor Position Report request (CSI 6 n)
		_, _ = emu.Write([]byte("\x1b[6n"))
		_ = emu.Close()

		<-done
		if gotErr != nil {
			t.Fatalf("read emulator input: %v", gotErr)
		}
		if len(got) != 0 {
			t.Fatalf("expected no reply bytes, got %q", got)
		}
	})

	t.Run("DA1_DA2", func(t *testing.T) {
		emu := vt.NewSafeEmulator(80, 40)
		disableVTQueriesThatBreakGuests(emu)

		var got []byte
		var gotErr error
		done := make(chan struct{})
		go func() {
			got, gotErr = drainAllWithTimeout(t, emu, 2*time.Second)
			close(done)
		}()

		// Device Attributes queries (CSI c and CSI > c)
		_, _ = emu.Write([]byte("\x1b[c"))
		_, _ = emu.Write([]byte("\x1b[>c"))
		_ = emu.Close()

		<-done
		if gotErr != nil {
			t.Fatalf("read emulator input: %v", gotErr)
		}
		if len(got) != 0 {
			t.Fatalf("expected no reply bytes, got %q", got)
		}
	})
}

func TestDisableVTQueriesThatBreakGuests_ChangesBehavior(t *testing.T) {
	// Sanity-check: the upstream emulator *does* emit reply bytes by default,
	// otherwise the "swallow" test above would be vacuous.
	emu := vt.NewSafeEmulator(80, 40)

	var got []byte
	var gotErr error
	done := make(chan struct{})
	go func() {
		got, gotErr = drainAllWithTimeout(t, emu, 2*time.Second)
		close(done)
	}()

	_, _ = emu.Write([]byte("\x1b[6n"))
	_ = emu.Close()

	<-done
	if gotErr != nil {
		t.Fatalf("read emulator input: %v", gotErr)
	}
	if len(got) == 0 {
		t.Fatalf("expected some reply bytes from default emulator, got none")
	}
}

func TestDrainAllWithTimeout(t *testing.T) {
	// Quick self-test: drain should return promptly on EOF.
	r := bytes.NewReader([]byte("ok"))
	b, err := drainAllWithTimeout(t, r, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "ok" {
		t.Fatalf("unexpected bytes: %q", b)
	}
}
