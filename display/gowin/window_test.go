package gowin

import "testing"

func TestPointerMapsRetinaAndLetterboxAndRejectsOutside(t *testing.T) {
	left, top, w, h := fit(2560, 1800, 2560, 1664)
	for _, test := range []struct {
		x, y         float32
		wantX, wantY int
		inside       bool
	}{
		{1280, 900, 1280, 832, true}, {0, 68, 0, 0, true}, {2559, 1731, 2559, 1663, true},
		{-10, -10, 0, 0, false}, {1280, 30, 1280, 0, false}, {2600, 1750, 2559, 1663, false},
	} {
		x, y, inside := pointerPosition(test.x, test.y, left, top, w, h, 2560, 1664)
		if x != test.wantX || y != test.wantY || inside != test.inside {
			t.Fatalf("%+v mapped to %d,%d,%v", test, x, y, inside)
		}
	}
	left, top, w, h = fit(1280, 900, 2560, 1664)
	x, y, inside := pointerPosition(640, 450, left, top, w, h, 2560, 1664)
	if x != 1280 || y != 832 || !inside {
		t.Fatalf("scaled center %d,%d,%v", x, y, inside)
	}
}
