package arm64

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// imageHeaderSizeBytes is the size in bytes of the ARM64 Image header as
	// documented in local/linux/Documentation/arch/arm64/booting.rst.
	imageHeaderSizeBytes = 64

	// The kernel must be placed text_offset bytes from a 2 MiB aligned base.
	imageLoadAlignment = 2 * 1024 * 1024

	arm64ImageMagic = 0x644d5241 // "ARM\x64"

	// maxGzipScanBytes bounds how far into the image we will search for a gzip
	// payload when the file starts with a self-decompression stub.
	maxGzipScanBytes = 1 << 20 // 1 MiB
)

// KernelHeader describes the 64-byte header placed at the beginning of every
// decompressed ARM64 Image.
type KernelHeader struct {
	Code0      uint32
	Code1      uint32
	TextOffset uint64
	ImageSize  uint64
	Flags      uint64
	Res2       uint64
	Res3       uint64
	Res4       uint64
	Magic      uint32
	Res5       uint32
}

// EntryPoint returns the address that the CPU should jump to relative to the
// provided 2 MiB aligned base address.
func (h KernelHeader) EntryPoint(base uint64) (uint64, error) {
	if base&(imageLoadAlignment-1) != 0 {
		return 0, fmt.Errorf("arm64 kernel base must be 2 MiB aligned (got %#x)", base)
	}
	return base + h.TextOffset, nil
}

// ImageProbe reports metadata gleaned from the kernel image without modifying
// guest memory.
type ImageProbe struct {
	Header             KernelHeader
	NeedsDecompression bool
	CompressedOffset   int64
}

// ProbeKernelImage inspects the supplied kernel image, parses its header, and
// determines if the payload needs to be decompressed before it can be used as
// an ARM64 Image.
func ProbeKernelImage(reader io.ReaderAt, size int64) (*ImageProbe, error) {
	if reader == nil {
		return nil, fmt.Errorf("arm64 probe requires a reader")
	}
	if size <= 0 {
		return nil, fmt.Errorf("arm64 kernel image size must be positive (got %d)", size)
	}

	header, err := readKernelHeaderAt(reader, 0)
	if err == nil {
		return &ImageProbe{Header: header}, nil
	}
	rawErr := err

	offset, err := findGzipPayload(reader, size)
	if err != nil {
		return nil, fmt.Errorf("arm64 kernel header not found: %w", rawErr)
	}

	header, err = readGzipKernelHeader(reader, offset, size)
	if err != nil {
		return nil, err
	}

	return &ImageProbe{
		Header:             header,
		NeedsDecompression: true,
		CompressedOffset:   offset,
	}, nil
}

func parseKernelHeader(header []byte) (KernelHeader, error) {
	if len(header) < imageHeaderSizeBytes {
		return KernelHeader{}, fmt.Errorf("arm64 kernel header truncated: got %d bytes", len(header))
	}

	h := KernelHeader{
		Code0:      binary.LittleEndian.Uint32(header[0:4]),
		Code1:      binary.LittleEndian.Uint32(header[4:8]),
		TextOffset: binary.LittleEndian.Uint64(header[8:16]),
		ImageSize:  binary.LittleEndian.Uint64(header[16:24]),
		Flags:      binary.LittleEndian.Uint64(header[24:32]),
		Res2:       binary.LittleEndian.Uint64(header[32:40]),
		Res3:       binary.LittleEndian.Uint64(header[40:48]),
		Res4:       binary.LittleEndian.Uint64(header[48:56]),
		Magic:      binary.LittleEndian.Uint32(header[56:60]),
		Res5:       binary.LittleEndian.Uint32(header[60:64]),
	}
	if h.Magic != arm64ImageMagic {
		return KernelHeader{}, fmt.Errorf("invalid arm64 kernel magic %#x", h.Magic)
	}
	return h, nil
}

func readKernelHeaderAt(reader io.ReaderAt, offset int64) (KernelHeader, error) {
	if offset < 0 {
		return KernelHeader{}, fmt.Errorf("negative header offset %d", offset)
	}
	buf := make([]byte, imageHeaderSizeBytes)
	if _, err := reader.ReadAt(buf, offset); err != nil {
		return KernelHeader{}, err
	}
	return parseKernelHeader(buf)
}

func findGzipPayload(reader io.ReaderAt, size int64) (int64, error) {
	if size < 2 {
		return 0, fmt.Errorf("kernel image too small to contain gzip header")
	}
	scan := size
	if scan > maxGzipScanBytes {
		scan = maxGzipScanBytes
	}

	buf := make([]byte, scan)
	n, err := reader.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("read kernel prefix: %w", err)
	}
	buf = buf[:n]

	magic := []byte{0x1f, 0x8b}
	idx := bytes.Index(buf, magic)
	if idx == -1 {
		return 0, fmt.Errorf("gzip header not found within first %d bytes", scan)
	}
	return int64(idx), nil
}

func readGzipKernelHeader(reader io.ReaderAt, offset, size int64) (KernelHeader, error) {
	if offset < 0 || offset >= size {
		return KernelHeader{}, fmt.Errorf("gzip offset %d outside kernel (size %d)", offset, size)
	}

	section := io.NewSectionReader(reader, offset, size-offset)
	gz, err := gzip.NewReader(section)
	if err != nil {
		return KernelHeader{}, fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close()

	buf := make([]byte, imageHeaderSizeBytes)
	if _, err := io.ReadFull(gz, buf); err != nil {
		return KernelHeader{}, fmt.Errorf("read gzip header: %w", err)
	}
	return parseKernelHeader(buf)
}

// ExtractImage returns the full Image payload, performing decompression when
// required. For raw Images the returned slice is the byte-for-byte kernel file.
func (p ImageProbe) ExtractImage(reader io.ReaderAt, size int64) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("arm64 extract requires a reader")
	}
	if size <= 0 {
		return nil, fmt.Errorf("arm64 kernel image size must be positive (got %d)", size)
	}

	if !p.NeedsDecompression {
		data := make([]byte, int(size))
		if _, err := reader.ReadAt(data, 0); err != nil {
			return nil, fmt.Errorf("read raw arm64 image: %w", err)
		}
		return data, nil
	}

	if p.CompressedOffset < 0 || p.CompressedOffset >= size {
		return nil, fmt.Errorf("arm64 compressed offset %d out of bounds (size %d)", p.CompressedOffset, size)
	}

	section := io.NewSectionReader(reader, p.CompressedOffset, size-p.CompressedOffset)
	gz, err := gzip.NewReader(section)
	if err != nil {
		return nil, fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decompress arm64 image: %w", err)
	}
	return data, nil
}
