package amd64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	headerMagicOffset  = 0x202
	headerMagic        = "HdrS"
	headerLengthOffset = 0x201
)

type SetupHeader struct {
	ProtocolVersion     uint16
	SetupSectors        uint8
	LoadFlags           uint8
	Code32Start         uint32
	RamdiskImage        uint32
	RamdiskSize         uint32
	HeapEndPtr          uint16
	CmdLinePtr          uint32
	InitrdAddrMax       uint32
	KernelAlignment     uint32
	RelocatableKernel   uint8
	MinAlignment        uint8
	XLoadFlags          uint16
	CmdlineSize         uint32
	HardwareSubarch     uint32
	HardwareSubarchData uint64
	PayloadOffset       uint32
	PayloadLength       uint32
	SetupData           uint64
	PrefAddress         uint64
	InitSize            uint32
	HandoverOffset      uint32
	KernelInfoOffset    uint32
}

// LoadBzImage reads kernelPath and validates the Linux/x86 boot header. It
// returns a KernelImage containing the raw image bytes, parsed setup header
// and a copy of the packed setup_header structure suitable for placing into
// the boot_params zero page.
func LoadBzImage(kernel io.ReaderAt, kernelSize int64) (*KernelImage, error) {
	data, err := io.ReadAll(io.NewSectionReader(kernel, 0, kernelSize))
	if err != nil {
		return nil, fmt.Errorf("read bzImage kernel: %w", err)
	}

	img := &KernelImage{
		format: kernelFormatBzImage,
		Data:   data,
	}

	if err := img.parseHeader(); err != nil {
		return nil, err
	}

	return img, nil
}

func (k *KernelImage) parseHeader() error {
	data := k.Data
	if len(data) < headerMagicOffset+4 {
		return errors.New("kernel image too small")
	}
	if string(data[headerMagicOffset:headerMagicOffset+4]) != headerMagic {
		return errors.New("missing HdrS signature; not a Linux bzImage")
	}

	headerLength := int(data[headerLengthOffset])
	headerEnd := headerMagicOffset + headerLength
	if headerEnd > len(data) {
		return errors.New("setup header extends past end of image")
	}
	if headerEnd <= setupHeaderOffset {
		return errors.New("invalid setup header length")
	}
	headerBytes := make([]byte, headerEnd-setupHeaderOffset)
	copy(headerBytes, data[setupHeaderOffset:headerEnd])
	k.HeaderBytes = headerBytes

	var hdr SetupHeader
	hdr.SetupSectors = data[setupHeaderOffset]
	if hdr.SetupSectors == 0 {
		hdr.SetupSectors = 4
	}
	hdr.ProtocolVersion = binary.LittleEndian.Uint16(data[protocolVersionOffset : protocolVersionOffset+2])
	hdr.LoadFlags = data[loadFlagsOffset]
	hdr.Code32Start = binary.LittleEndian.Uint32(data[code32StartOffset : code32StartOffset+4])
	hdr.RamdiskImage = binary.LittleEndian.Uint32(data[ramdiskImageOffset : ramdiskImageOffset+4])
	hdr.RamdiskSize = binary.LittleEndian.Uint32(data[ramdiskSizeOffset : ramdiskSizeOffset+4])
	hdr.HeapEndPtr = binary.LittleEndian.Uint16(data[heapEndPtrOffset : heapEndPtrOffset+2])
	hdr.CmdLinePtr = binary.LittleEndian.Uint32(data[cmdLinePtrOffset : cmdLinePtrOffset+4])
	hdr.InitrdAddrMax = binary.LittleEndian.Uint32(data[initrdAddrMaxOffset : initrdAddrMaxOffset+4])
	hdr.KernelAlignment = binary.LittleEndian.Uint32(data[kernelAlignmentOffset : kernelAlignmentOffset+4])
	hdr.RelocatableKernel = data[relocatableKernelOffset]
	hdr.MinAlignment = data[minAlignmentOffset]
	hdr.XLoadFlags = binary.LittleEndian.Uint16(data[xloadflagsOffset : xloadflagsOffset+2])
	hdr.CmdlineSize = binary.LittleEndian.Uint32(data[cmdlineSizeOffset : cmdlineSizeOffset+4])
	hdr.HardwareSubarch = binary.LittleEndian.Uint32(data[hardwareSubarchOffset : hardwareSubarchOffset+4])
	hdr.HardwareSubarchData = binary.LittleEndian.Uint64(data[hardwareSubarchDataOffset : hardwareSubarchDataOffset+8])
	hdr.PayloadOffset = binary.LittleEndian.Uint32(data[payloadOffsetOffset : payloadOffsetOffset+4])
	hdr.PayloadLength = binary.LittleEndian.Uint32(data[payloadLengthOffset : payloadLengthOffset+4])
	hdr.SetupData = binary.LittleEndian.Uint64(data[setupDataOffset : setupDataOffset+8])
	hdr.PrefAddress = binary.LittleEndian.Uint64(data[prefAddressOffset : prefAddressOffset+8])
	hdr.InitSize = binary.LittleEndian.Uint32(data[initSizeOffset : initSizeOffset+4])
	hdr.HandoverOffset = binary.LittleEndian.Uint32(data[handoverOffsetOffset : handoverOffsetOffset+4])
	hdr.KernelInfoOffset = binary.LittleEndian.Uint32(data[kernelInfoOffsetOffset : kernelInfoOffsetOffset+4])

	k.Header = hdr

	setupSectors := int(hdr.SetupSectors)
	payloadOffset := 512 * (1 + setupSectors)
	if payloadOffset > len(data) {
		return fmt.Errorf("payload offset %d exceeds image size %d", payloadOffset, len(data))
	}
	k.PayloadOffset = payloadOffset

	if hdr.XLoadFlags&0x1 == 0 {
		return errors.New("kernel does not advertise 64-bit entry (XLF_KERNEL_64)")
	}
	return nil
}

// Payload returns the compressed protected-mode payload of the kernel image.
func (k *KernelImage) Payload() []byte {
	if k.format != kernelFormatBzImage {
		return nil
	}
	return k.Data[k.PayloadOffset:]
}
