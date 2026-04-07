package fdt

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildProducesValidHeader(t *testing.T) {
	blob, err := Build(Node{
		Name: "",
		Properties: map[string]Property{
			"compatible": {Strings: []string{"ccx3,test"}},
		},
		Children: []Node{
			{
				Name: "chosen",
				Properties: map[string]Property{
					"bootargs": {Strings: []string{"console=ttyS0"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(blob) < 0x28 {
		t.Fatalf("blob too small: %d", len(blob))
	}
	if got := binary.BigEndian.Uint32(blob[0:4]); got != magic {
		t.Fatalf("magic = %#x, want %#x", got, magic)
	}
	if got := binary.BigEndian.Uint32(blob[4:8]); got != uint32(len(blob)) {
		t.Fatalf("totalsize = %d, want %d", got, len(blob))
	}
}

func TestBuildContainsPropertyStrings(t *testing.T) {
	blob, err := Build(Node{
		Name: "",
		Properties: map[string]Property{
			"compatible": {Strings: []string{"ccx3,test"}},
			"model":      {Strings: []string{"ccx3"}},
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !bytes.Contains(blob, []byte("compatible\x00")) {
		t.Fatal("blob missing compatible string name")
	}
	if !bytes.Contains(blob, []byte("ccx3,test\x00")) {
		t.Fatal("blob missing compatible property value")
	}
}

func TestBuildRejectsInvalidProperty(t *testing.T) {
	_, err := Build(Node{
		Name: "",
		Properties: map[string]Property{
			"bad": {Strings: []string{"x"}, U32: []uint32{1}},
		},
	})
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
}
