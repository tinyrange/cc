package kernel

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"
)

// Module index binary format constants (from kmod/libkmod)
const (
	indexMagic   = 0xB007F457
	indexVersion = 0x00020001

	// Node flags encoded in upper nibble of offset
	indexNodePrefix = 0x80000000
	indexNodeValues = 0x40000000
	indexNodeChilds = 0x20000000
)

// indexValue represents a value with priority in the trie
type indexValue struct {
	value    string
	priority uint32
}

// indexNode represents a node in the trie
type indexNode struct {
	prefix   string
	values   []indexValue
	children [256]*indexNode
}

// indexBuilder builds a binary module index
type indexBuilder struct {
	root *indexNode
}

// normalizeModuleName converts hyphens to underscores (kmod convention)
func normalizeModuleName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

// newIndexBuilder creates a new index builder
func newIndexBuilder() *indexBuilder {
	return &indexBuilder{
		root: &indexNode{},
	}
}

// add inserts a key-value pair into the trie
func (b *indexBuilder) add(key, value string, priority uint32) {
	node := b.root
	keyPos := 0

	for keyPos < len(key) {
		c := key[keyPos]
		child := node.children[c]

		if child == nil {
			// Create new node with remaining key as prefix
			child = &indexNode{
				prefix: key[keyPos+1:],
			}
			node.children[c] = child
			child.values = append(child.values, indexValue{value, priority})
			return
		}

		// Check how much of child's prefix matches
		prefixLen := len(child.prefix)
		remaining := key[keyPos+1:]
		matchLen := 0

		for matchLen < prefixLen && matchLen < len(remaining) {
			if child.prefix[matchLen] != remaining[matchLen] {
				break
			}
			matchLen++
		}

		if matchLen < prefixLen {
			// Need to split the node
			// Create new intermediate node
			newChild := &indexNode{
				prefix:   child.prefix[:matchLen],
				children: [256]*indexNode{},
			}

			// Move old child under new child
			splitChar := child.prefix[matchLen]
			child.prefix = child.prefix[matchLen+1:]
			newChild.children[splitChar] = child

			// Replace in parent
			node.children[c] = newChild

			if matchLen < len(remaining) {
				// Add new branch for remaining key
				newBranch := &indexNode{
					prefix: remaining[matchLen+1:],
				}
				newBranch.values = append(newBranch.values, indexValue{value, priority})
				newChild.children[remaining[matchLen]] = newBranch
			} else {
				// Value goes on the intermediate node
				newChild.values = append(newChild.values, indexValue{value, priority})
			}
			return
		}

		// Full prefix match, continue down
		keyPos += 1 + prefixLen
		node = child
	}

	// Key exhausted, add value to current node
	node.values = append(node.values, indexValue{value, priority})
}

// build serializes the trie to binary format
func (b *indexBuilder) build() []byte {
	buf := new(bytes.Buffer)

	// Write header
	binary.Write(buf, binary.BigEndian, uint32(indexMagic))
	binary.Write(buf, binary.BigEndian, uint32(indexVersion))
	binary.Write(buf, binary.BigEndian, uint32(0)) // root offset placeholder

	// Serialize the trie
	rootOffset := b.serializeNode(buf, b.root)

	// Update root offset in header
	result := buf.Bytes()
	binary.BigEndian.PutUint32(result[8:], rootOffset)

	return result
}

// serializeNode serializes a node and its children, returns offset with flags
func (b *indexBuilder) serializeNode(buf *bytes.Buffer, node *indexNode) uint32 {
	if node == nil {
		return 0
	}

	// First, recursively serialize all children to get their offsets
	var childOffsets [256]uint32
	var firstChild, lastChild int = -1, -1

	for i := 32; i < 128; i++ {
		if node.children[i] != nil {
			childOffsets[i] = b.serializeNode(buf, node.children[i])
			if firstChild < 0 {
				firstChild = i
			}
			lastChild = i
		}
	}

	// Now serialize this node
	nodeOffset := uint32(buf.Len())
	flags := uint32(0)

	// Write prefix if present
	if len(node.prefix) > 0 {
		flags |= indexNodePrefix
		buf.WriteString(node.prefix)
		buf.WriteByte(0) // null terminator
	}

	// Write children if present
	if firstChild >= 0 {
		flags |= indexNodeChilds
		buf.WriteByte(byte(firstChild))
		buf.WriteByte(byte(lastChild))
		for i := firstChild; i <= lastChild; i++ {
			binary.Write(buf, binary.BigEndian, childOffsets[i])
		}
	}

	// Write values if present
	if len(node.values) > 0 {
		flags |= indexNodeValues
		// Write value count first
		binary.Write(buf, binary.BigEndian, uint32(len(node.values)))
		for _, v := range node.values {
			binary.Write(buf, binary.BigEndian, v.priority)
			buf.WriteString(v.value)
			buf.WriteByte(0) // null terminator
		}
	}

	return nodeOffset | flags
}

// generateBinaryIndexes generates .bin files from text metadata files
func generateBinaryIndexes(files []ModuleFile) []ModuleFile {
	var result []ModuleFile

	// Collect text files we need
	var depData, aliasData, symbolsData, softdepData, builtinData, builtinModinfoData []byte

	for _, f := range files {
		base := filepath.Base(f.Path)
		switch base {
		case "modules.dep":
			depData = f.Data
		case "modules.alias":
			aliasData = f.Data
		case "modules.symbols":
			symbolsData = f.Data
		case "modules.softdep":
			softdepData = f.Data
		case "modules.builtin":
			builtinData = f.Data
		case "modules.builtin.modinfo":
			builtinModinfoData = f.Data
		}
	}

	// Generate modules.dep.bin
	if depData != nil {
		binData := buildDepIndex(depData)
		result = append(result, ModuleFile{
			Path: "modules.dep.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	// Generate modules.alias.bin
	if aliasData != nil {
		binData := buildAliasIndex(aliasData)
		result = append(result, ModuleFile{
			Path: "modules.alias.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	// Generate modules.symbols.bin
	if symbolsData != nil {
		binData := buildSymbolsIndex(symbolsData)
		result = append(result, ModuleFile{
			Path: "modules.symbols.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	// Generate modules.softdep.bin
	if softdepData != nil {
		binData := buildSoftdepIndex(softdepData)
		result = append(result, ModuleFile{
			Path: "modules.softdep.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	// Generate modules.builtin.bin
	if builtinData != nil {
		binData := buildBuiltinIndex(builtinData)
		result = append(result, ModuleFile{
			Path: "modules.builtin.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	// Generate modules.builtin.alias.bin from modules.builtin.modinfo
	if builtinModinfoData != nil {
		binData := buildBuiltinAliasIndex(builtinModinfoData)
		result = append(result, ModuleFile{
			Path: "modules.builtin.alias.bin",
			Data: binData,
			Mode: 0644,
			Size: int64(len(binData)),
		})
	}

	return result
}

// buildDepIndex builds binary index from modules.dep
// Format: module_path: dep1 dep2 dep3
// Key is module name (without path and .ko), value is full line (path: deps)
func buildDepIndex(data []byte) []byte {
	builder := newIndexBuilder()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Split on first colon
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}

		modPath := line[:idx]

		// Extract module name from path: "kernel/drivers/block/loop.ko" -> "loop"
		baseName := filepath.Base(modPath)
		modName := strings.TrimSuffix(baseName, ".ko")
		modName = strings.TrimSuffix(modName, ".ko.xz")
		modName = strings.TrimSuffix(modName, ".ko.zst")
		// Normalize: replace hyphens with underscores (kmod convention)
		modName = normalizeModuleName(modName)

		// Key is module name, value is the full line (path: deps)
		builder.add(modName, line, 0)
	}

	return builder.build()
}

// buildAliasIndex builds binary index from modules.alias
// Format: alias alias_pattern module_name
func buildAliasIndex(data []byte) []byte {
	builder := newIndexBuilder()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "alias" {
			continue
		}

		// Key is the alias pattern, value is the module name
		key := fields[1]
		value := fields[2]
		builder.add(key, value, 0)
	}

	return builder.build()
}

// buildSymbolsIndex builds binary index from modules.symbols
// Format: alias symbol:symbol_name module_name
func buildSymbolsIndex(data []byte) []byte {
	builder := newIndexBuilder()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "alias" {
			continue
		}

		// Key is the symbol (with symbol: prefix), value is the module name
		key := fields[1]
		value := fields[2]
		builder.add(key, value, 0)
	}

	return builder.build()
}

// buildSoftdepIndex builds binary index from modules.softdep
// Format: softdep module_name pre: dep1 dep2 post: dep3 dep4
func buildSoftdepIndex(data []byte) []byte {
	builder := newIndexBuilder()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "softdep" {
			continue
		}

		// Key is the module name, value is the rest of the line
		key := fields[1]
		value := strings.TrimPrefix(line, "softdep "+key)
		value = strings.TrimSpace(value)
		builder.add(key, value, 0)
	}

	return builder.build()
}

// buildBuiltinIndex builds binary index from modules.builtin
// Format: one module path per line (e.g., kernel/fs/ext4/ext4.ko)
func buildBuiltinIndex(data []byte) []byte {
	builder := newIndexBuilder()
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Key is the module path, value is empty (just marks it as builtin)
		builder.add(line, "", 0)
	}

	return builder.build()
}

// buildBuiltinAliasIndex builds binary index from modules.builtin.modinfo
// Format: null-separated entries like "module.alias=alias_pattern"
func buildBuiltinAliasIndex(data []byte) []byte {
	builder := newIndexBuilder()

	// Split on null bytes
	entries := bytes.Split(data, []byte{0})

	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}

		line := string(entry)

		// Look for .alias= entries
		idx := strings.Index(line, ".alias=")
		if idx < 0 {
			continue
		}

		// Extract module name and alias
		moduleName := line[:idx]
		aliasValue := line[idx+7:] // skip ".alias="

		// Key is the alias, value is the module name
		builder.add(aliasValue, moduleName, 0)
	}

	return builder.build()
}
