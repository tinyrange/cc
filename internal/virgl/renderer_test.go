package virgl

import (
	"fmt"
	"testing"

	"j5.nz/cc/internal/virtio"
)

type transferBacking []byte

func (b transferBacking) Size() uint64 { return uint64(len(b)) }

func (b transferBacking) ReadAt(offset uint64, destination []byte) error {
	end := offset + uint64(len(destination))
	if end < offset || end > uint64(len(b)) {
		return fmt.Errorf("read %d..%d exceeds %d", offset, end, len(b))
	}
	copy(destination, b[offset:end])
	return nil
}

func (b transferBacking) WriteAt(offset uint64, source []byte) error {
	end := offset + uint64(len(source))
	if end < offset || end > uint64(len(b)) {
		return fmt.Errorf("write %d..%d exceeds %d", offset, end, len(b))
	}
	copy(b[offset:end], source)
	return nil
}

func TestZeroTextureTransferStrideUsesFullMipWidth(t *testing.T) {
	description := virtio.GPUResource3D{
		ID: 1, Target: 2, Format: 67,
		Width: 4, Height: 4, Depth: 1, ArraySize: 1,
	}
	transfer := virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 1, Y: 1, Width: 2, Height: 2, Depth: 1},
	}

	got, err := transferDataSize(description, transfer)
	if err != nil {
		t.Fatal(err)
	}
	// Two RGBA pixels from the first row, one full four-pixel image stride,
	// then two pixels from the final row.
	if want := uint64(2*4 + 4*4); got != want {
		t.Fatalf("zero-stride partial texture transfer size = %d, want %d", got, want)
	}
}

func TestPartialTextureTransferGathersRowsFromFullGuestStride(t *testing.T) {
	description := virtio.GPUResource3D{
		ID: 1, Target: 2, Format: 67,
		Width: 4, Height: 2, Depth: 1, ArraySize: 1,
	}
	backing := transferBacking{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
	}
	transfer := virtio.GPUTransfer3D{
		ResourceID: description.ID,
		Box:        virtio.GPUBox{X: 1, Width: 2, Height: 2, Depth: 1},
		Offset:     4,
		Backing:    backing,
	}

	data, normalized, err := stageTransferData(description, transfer)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		4, 5, 6, 7, 8, 9, 10, 11,
		20, 21, 22, 23, 24, 25, 26, 27,
	}
	if string(data) != string(want) {
		t.Fatalf("staged partial texture rows = %v, want %v", data, want)
	}
	if normalized.Offset != 0 || normalized.Stride != 8 || normalized.LayerStride != 16 {
		t.Fatalf("normalized transfer = offset %d stride %d layer stride %d, want 0, 8, 16",
			normalized.Offset, normalized.Stride, normalized.LayerStride)
	}
}
