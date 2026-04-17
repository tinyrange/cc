package nifti

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Header struct {
	Dim [8]int16
}

func ReadHeader(path string) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") || strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".nii.gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return Header{}, err
		}
		defer gz.Close()
		r = gz
	}

	buf := make([]byte, 348)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Header{}, err
	}

	var order binary.ByteOrder = binary.LittleEndian
	if int32(order.Uint32(buf[0:4])) != 348 {
		order = binary.BigEndian
		if int32(order.Uint32(buf[0:4])) != 348 {
			return Header{}, fmt.Errorf("unsupported NIfTI header size")
		}
	}

	var hdr Header
	for i := 0; i < 8; i++ {
		hdr.Dim[i] = int16(order.Uint16(buf[40+i*2 : 42+i*2]))
	}
	return hdr, nil
}

func Shape(h Header) []int {
	n := int(h.Dim[0])
	if n < 0 {
		n = 0
	}
	if n > 7 {
		n = 7
	}
	out := make([]int, 0, n)
	for i := 1; i <= n; i++ {
		if h.Dim[i] <= 0 {
			break
		}
		out = append(out, int(h.Dim[i]))
	}
	return out
}

func Is3D(h Header) bool {
	shape := Shape(h)
	if len(shape) == 0 {
		return false
	}
	nonUnit := 0
	for _, d := range shape {
		if d > 1 {
			nonUnit++
		}
	}
	return nonUnit <= 3
}
