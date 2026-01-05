package arm64

import (
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/timeslice"
)

// KernelImage represents a fully parsed ARM64 Linux kernel image.
type KernelImage struct {
	Header  KernelHeader
	payload []byte
}

var (
	tsLinuxLoaderArm64ProbeKernel   = timeslice.RegisterKind("linux_loader_arm64_probe_kernel", 0)
	tsLinuxLoaderArm64ExtractKernel = timeslice.RegisterKind("linux_loader_arm64_extract_kernel", 0)
)

// LoadKernel parses the supplied ARM64 Image (optionally compressed) and
// returns an in-memory representation ready for placement into guest RAM.
func LoadKernel(reader io.ReaderAt, size int64) (*KernelImage, error) {
	rec := timeslice.NewState()

	probe, err := ProbeKernelImage(reader, size)
	if err != nil {
		return nil, fmt.Errorf("probe arm64 kernel: %w", err)
	}

	rec.Record(tsLinuxLoaderArm64ProbeKernel)

	payload, err := probe.ExtractImage(reader, size)
	if err != nil {
		return nil, fmt.Errorf("extract arm64 kernel image: %w", err)
	}
	if len(payload) < imageHeaderSizeBytes {
		return nil, fmt.Errorf("arm64 kernel image too small (%d bytes)", len(payload))
	}

	rec.Record(tsLinuxLoaderArm64ExtractKernel)

	return &KernelImage{
		Header:  probe.Header,
		payload: payload,
	}, nil
}

// Payload returns the raw Image bytes as they should appear in guest RAM.
func (k *KernelImage) Payload() []byte {
	if k == nil {
		return nil
	}
	return k.payload
}
