//go:build windows && amd64

package whp

import (
	"encoding/binary"
	"fmt"
	"io"
)

func writeArm64GICState(w io.Writer, gicState *arm64GICSnapshot) error {
	// On AMD64, GIC state is always nil - just write the absence flag
	if err := binary.Write(w, binary.LittleEndian, uint8(0)); err != nil {
		return fmt.Errorf("write gic present flag: %w", err)
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

	// On AMD64, we shouldn't have GIC state - skip it if present
	var dataLen uint32
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("read gic data length: %w", err)
	}

	// Skip the data
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("skip gic data: %w", err)
	}

	return nil, nil
}
