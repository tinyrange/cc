package virgl

import (
	"encoding/binary"
	"testing"
)

func TestCapsetExposesRenderableDepthFormatsForEGLConfigs(t *testing.T) {
	capset := buildCapsetV1()
	const (
		samplerOffset = 4
		renderOffset  = samplerOffset + 64
		depthOffset   = renderOffset + 64
	)
	for _, format := range []uint32{16, 18, 19, 21} {
		for _, offset := range []int{samplerOffset, renderOffset, depthOffset} {
			word := binary.LittleEndian.Uint32(capset[offset+int(format/32)*4:])
			if word&(1<<(format%32)) == 0 {
				t.Fatalf("format %d is absent from mask at byte %d", format, offset)
			}
		}
	}
}
