package vm

import (
	"bytes"
	"reflect"
	"testing"
)

// TestFragmentedRegionReadAt tests the ReadAt method of fragmentedRegion.
func TestFragmentedRegionReadAt(t *testing.T) {
	fragRegion := newFragmentRegion(16)
	region := RawRegion(make([]byte, 16))
	copy(region, []byte("Hello, World!"))
	_ = fragRegion.mapFragment(&region, 0)

	buff := make([]byte, 5)
	n, err := fragRegion.ReadAt(buff, 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("ReadAt = %d, want 5", n)
	}
	expected := []byte("Hello")
	if !reflect.DeepEqual(buff, expected) {
		t.Errorf("ReadAt buffer = %v, want %v", buff, expected)
	}
}

// TestFragmentedRegionWriteAt tests the WriteAt method of fragmentedRegion.
func TestFragmentedRegionWriteAt(t *testing.T) {
	fragRegion := newFragmentRegion(16)
	data := []byte("Goodbye!")
	n, err := fragRegion.WriteAt(data, 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("WriteAt = %d, want %d", n, len(data))
	}

	buff := make([]byte, 8)
	n, err = fragRegion.ReadAt(buff, 0)
	if err != nil {
		t.Errorf("unexpected error during read: %v", err)
	}
	if n != 8 {
		t.Errorf("ReadAt after WriteAt = %d, want 8", n)
	}
	if !bytes.Equal(buff, data) {
		t.Errorf("ReadAt buffer after WriteAt = %v, want %v", buff, data)
	}
}

// TestFragmentedRegionMapFragment tests the mapFragment method of fragmentedRegion.
func TestFragmentedRegionMapFragment(t *testing.T) {
	fragRegion := newFragmentRegion(32)
	addRegion := RawRegion([]byte("Gophers!"))
	err := fragRegion.mapFragment(&addRegion, 8)
	if err != nil {
		t.Errorf("mapFragment error: %v", err)
	}

	buff := make([]byte, 8)
	n, err := fragRegion.ReadAt(buff, 8)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(buff) {
		t.Errorf("ReadAt = %d, want %d", n, len(buff))
	}

	t.Logf("buff %+v", buff)

	expected := []byte("Gophers!")
	if !reflect.DeepEqual(buff, expected) {
		t.Errorf("ReadAt buffer after mapFragment = %v, want %v", buff, expected)
	}
}
