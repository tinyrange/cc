//go:build windows && arm64

package whp

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

func init() {
	// Register types for gob encoding
	gob.Register(arm64GICSnapshot{})
	gob.Register(bindings.Arm64LocalInterruptControllerState{})
	gob.Register(bindings.Arm64GlobalInterruptControllerStateHeader{})
	gob.Register(bindings.Arm64GlobalInterruptState{})
	gob.Register(bindings.Arm64InterruptState{})
}

func writeArm64GICState(w io.Writer, gicState *arm64GICSnapshot) error {
	// Write presence flag
	if gicState == nil {
		if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil {
			return fmt.Errorf("write gic present flag: %w", err)
		}
		return nil
	}

	if err := binary.Write(w, binary.LittleEndian, uint8(1)); err != nil {
		return fmt.Errorf("write gic present flag: %w", err)
	}

	// Encode GIC state with gob
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(gicState); err != nil {
		return fmt.Errorf("gob encode gic state: %w", err)
	}

	// Write encoded data length and data
	if err := binary.Write(w, binary.LittleEndian, uint32(buf.Len())); err != nil {
		return fmt.Errorf("write gic data length: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write gic data: %w", err)
	}

	return nil
}

func readArm64GICState(r io.Reader) (*arm64GICSnapshot, error) {
	// Read presence flag
	var present uint8
	if err := binary.Read(r, binary.LittleEndian, &present); err != nil {
		// EOF is acceptable - older snapshots may not have GIC state
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("read gic present flag: %w", err)
	}

	if present == 0 {
		return nil, nil
	}

	// Read encoded data length
	var dataLen uint32
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("read gic data length: %w", err)
	}

	// Read encoded data
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read gic data: %w", err)
	}

	// Decode with gob
	var gicState arm64GICSnapshot
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&gicState); err != nil {
		return nil, fmt.Errorf("gob decode gic state: %w", err)
	}

	return &gicState, nil
}
