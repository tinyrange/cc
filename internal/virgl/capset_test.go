package virgl

import (
	"encoding/binary"
	"math"
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

func TestCapsetAdvertisesImplementedArrayTexturesAndMultipleRenderTargets(t *testing.T) {
	capset := buildCapsetV1()
	const (
		maxArrayLayersOffset   = 4 + 64*4 + 4 + 4
		maxRenderTargetsOffset = 4 + 64*4 + 4 + 16
		maxUniformBlocksOffset = 4 + 64*4 + 4 + 32
	)
	if got := binary.LittleEndian.Uint32(capset[maxArrayLayersOffset:]); got != 256 {
		t.Fatalf("maximum array texture layers = %d, want 256", got)
	}
	if got := binary.LittleEndian.Uint32(capset[maxRenderTargetsOffset:]); got != 4 {
		t.Fatalf("maximum render targets = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint32(capset[maxUniformBlocksOffset:]); got != 12 {
		t.Fatalf("maximum uniform blocks = %d, want 12", got)
	}
}

func TestCapsetV2KeepsModernFeatureFallbacksDisabled(t *testing.T) {
	capset := buildCapsetV2()
	if got := len(capset); got != capsetV2Size {
		t.Fatalf("capset v2 size = %d, want %d", got, capsetV2Size)
	}
	if got := binary.LittleEndian.Uint32(capset[0:4]); got != capsetVersion2 {
		t.Fatalf("capset v2 max version = %d, want %d", got, capsetVersion2)
	}
	const (
		maxTexture2DSizeOffset   = capsetV2LimitsStart + 176
		hostFeatureVersionOffset = capsetV2LimitsStart + 248
		rendererOffset           = capsetV2LimitsStart + 388
		maxAnisotropyOffset      = rendererOffset + 64
	)
	if got := binary.LittleEndian.Uint32(capset[maxTexture2DSizeOffset:]); got != 4096 {
		t.Fatalf("capset v2 max 2D texture size = %d, want 4096", got)
	}
	if got := binary.LittleEndian.Uint32(capset[hostFeatureVersionOffset:]); got != 0 {
		t.Fatalf("capset v2 host feature check version = %d, want 0", got)
	}
	if got := string(capset[rendererOffset : rendererOffset+17]); got != "vmsh Darwin VirGL" {
		t.Fatalf("capset v2 renderer = %q", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(capset[maxAnisotropyOffset:])); got != 1 {
		t.Fatalf("capset v2 max anisotropy = %v, want 1", got)
	}
}

func TestRendererPublishesBothVirGLCapsetGenerations(t *testing.T) {
	capsets := NewRenderer(nil).Capsets()
	if len(capsets) != 2 {
		t.Fatalf("renderer capset count = %d, want 2", len(capsets))
	}
	if capsets[0].ID != capsetVirGL || capsets[0].Version != capsetVersion {
		t.Fatalf("legacy capset identity = (%d, %d)", capsets[0].ID, capsets[0].Version)
	}
	if capsets[1].ID != capsetVirGL2 || capsets[1].Version != capsetVersion2 {
		t.Fatalf("v2 capset identity = (%d, %d)", capsets[1].ID, capsets[1].Version)
	}
}
