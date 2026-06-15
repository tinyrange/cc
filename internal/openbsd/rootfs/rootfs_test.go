package rootfs

import "testing"

func TestIsBuiltInImage(t *testing.T) {
	for _, image := range []string{"@openbsd", "openbsd", "  @OpenBSD  "} {
		if !IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = false", image)
		}
	}
	for _, image := range []string{"", "alpine", "@linux", "openbsd:7.9"} {
		if IsBuiltInImage(image) {
			t.Fatalf("IsBuiltInImage(%q) = true", image)
		}
	}
}
