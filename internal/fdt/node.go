package fdt

// Property describes a single device-tree property in a JSON-friendly form.
// Exactly one of the typed fields should be populated for a given property.
type Property struct {
	Strings []string `json:"strings,omitempty"`
	U32     []uint32 `json:"u32,omitempty"`
	U64     []uint64 `json:"u64,omitempty"`
	Bytes   []byte   `json:"bytes,omitempty"`
	Flag    bool     `json:"flag,omitempty"`
}

// Kind returns the name of the populated field or an empty string if none are set.
func (p Property) Kind() string {
	switch {
	case len(p.Strings) > 0:
		return "strings"
	case len(p.U32) > 0:
		return "u32"
	case len(p.U64) > 0:
		return "u64"
	case len(p.Bytes) > 0:
		return "bytes"
	case p.Flag:
		return "flag"
	default:
		return ""
	}
}

// DefinedCount reports how many distinct fields on the property are populated.
func (p Property) DefinedCount() int {
	count := 0
	if len(p.Strings) > 0 {
		count++
	}
	if len(p.U32) > 0 {
		count++
	}
	if len(p.U64) > 0 {
		count++
	}
	if len(p.Bytes) > 0 {
		count++
	}
	if p.Flag {
		count++
	}
	return count
}

// Node describes a device-tree node using JSON-friendly structures.
type Node struct {
	Name       string              `json:"name"`
	Properties map[string]Property `json:"properties,omitempty"`
	Children   []Node              `json:"children,omitempty"`
}
