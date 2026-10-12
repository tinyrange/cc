package kernelvm

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
)

// Disk is borrowed portable NVMe backing, never a host filename.
type Disk interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}
type ConsoleStep struct{ Wait, Send string }
type BSDOptions struct {
	OS       string
	MemoryMB uint64
	Marker   string
	Root     Disk
	Console  []ConsoleStep
}

// RunBSD directly loads an amd64 BSD kernel, with an optional NVMe root disk,
// and stops at Marker. It always closes the VM before returning. A context
// deadline is mandatory; the serial transcript is capped at 1 MiB.
func RunBSD(ctx context.Context, kernel []byte, opts BSDOptions) (string, error) {
	if opts.OS != "openbsd" && opts.OS != "freebsd" && opts.OS != "netbsd" {
		return "", fmt.Errorf("BSD OS must be openbsd, freebsd or netbsd")
	}
	if len(kernel) == 0 || len(kernel) > 256<<20 {
		return "", fmt.Errorf("BSD kernel must be nonempty and at most 256 MiB")
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", fmt.Errorf("BSD boot requires a context deadline")
	}
	if strings.TrimSpace(opts.Marker) == "" || len(opts.Marker) > 4096 {
		return "", fmt.Errorf("BSD serial marker must be nonempty and at most 4096 bytes")
	}
	if opts.MemoryMB == 0 {
		opts.MemoryMB = 1024
	}
	if opts.MemoryMB < 64 || opts.MemoryMB > 8192 {
		return "", fmt.Errorf("BSD memory must be 64..8192 MiB")
	}
	if opts.Root != nil && (opts.Root.Size() < 512 || opts.Root.Size()%512 != 0) {
		return "", fmt.Errorf("BSD root disk must contain complete 512-byte sectors")
	}
	if len(opts.Console) > 64 {
		return "", fmt.Errorf("BSD console sequence exceeds 64 steps")
	}
	for _, step := range opts.Console {
		if step.Wait == "" || len(step.Wait) > 4096 || len(step.Send) > 4096 {
			return "", fmt.Errorf("invalid BSD console step")
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// OpenBSD installer kernels may be gzip-compressed. Decode once with a hard
	// ceiling instead of letting an untrusted stream allocate without a bound.
	if len(kernel) >= 2 && kernel[0] == 0x1f && kernel[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(kernel))
		if err != nil {
			return "", fmt.Errorf("BSD kernel gzip: %w", err)
		}
		decoded, err := io.ReadAll(io.LimitReader(reader, (256<<20)+1))
		closeErr := reader.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		if len(decoded) > 256<<20 {
			return "", fmt.Errorf("BSD decoded kernel exceeds 256 MiB")
		}
		kernel = decoded
	}
	if err := validateBSDKernel(kernel); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return runBSD(ctx, kernel, opts)
}
