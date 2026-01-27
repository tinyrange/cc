package vfs

import (
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/fslayer"
)

// SnapshotOptions configures snapshot capture behavior.
type SnapshotOptions struct {
	// Excludes contains path patterns to exclude from the snapshot.
	// Patterns use glob-style matching (*, ?, []).
	Excludes []string
}

// CaptureLayer captures all modifications made to the filesystem since boot
// (or since the last layer freeze) and returns them as LayerData.
func (v *virtioFsBackend) CaptureLayer(opts *SnapshotOptions) (*fslayer.LayerData, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()

	data := &fslayer.LayerData{}

	// Capture all modified nodes by walking all directory entries.
	// This approach correctly handles hardlinks, which create multiple
	// directory entries pointing to the same node ID.
	//
	// We iterate over all nodes that are directories and look at their
	// entries map. For each entry, if the target node is modified, we
	// capture it under the path derived from the directory entry (not
	// from the node's name/parent which only stores the first name).
	captured := make(map[string]bool) // paths already captured

	for _, dirNode := range v.nodes {
		if !dirNode.isDir() {
			continue
		}

		dirPath := v.buildNodePath(dirNode)

		// Check each entry in this directory
		for name, nodeID := range dirNode.entries {
			node := v.nodes[nodeID]
			if node == nil {
				continue
			}

			entryPath := path.Join(dirPath, name)
			if entryPath == "" {
				entryPath = "/" + name
			}

			// Skip if already captured (same path)
			if captured[entryPath] {
				continue
			}

			// Check exclusions
			if opts != nil && shouldExclude(entryPath, opts.Excludes) {
				continue
			}

			// Capture if modified
			if node.isModified() {
				entry, err := v.nodeToLayerEntry(node, entryPath)
				if err == nil {
					data.Entries = append(data.Entries, entry)
					captured[entryPath] = true
				}
			}
		}
	}

	// Also collect deleted entries from directories with abstract backing
	for _, node := range v.nodes {
		if node.abstractDir != nil && node.deletedEntries != nil {
			dirPath := v.buildNodePath(node)
			for name := range node.deletedEntries {
				deletedPath := path.Join(dirPath, name)
				if opts != nil && shouldExclude(deletedPath, opts.Excludes) {
					continue
				}
				data.Entries = append(data.Entries, fslayer.LayerEntry{
					Path:    deletedPath,
					Kind:    fslayer.LayerEntryDeleted,
					ModTime: time.Now(),
				})
			}
		}
	}

	return data, nil
}

// FreezeCurrentLayer marks the current state as a layer boundary.
// Future modifications will be tracked in a new layer.
// Returns the layer ID that was frozen.
func (v *virtioFsBackend) FreezeCurrentLayer() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	// For now, this is a no-op since we track all modifications
	// Future enhancement: track layer boundaries for incremental snapshots
	return 0
}

// isModified returns true if this node has in-memory modifications
// that differ from its abstract backing (or has no abstract backing).
func (n *fsNode) isModified() bool {
	// Node with in-memory blocks has been written to
	if n.blocks != nil && len(n.blocks) > 0 {
		return true
	}

	// Node without abstract backing is newly created
	if n.abstractFile == nil && n.abstractDir == nil {
		// Any node without abstract backing is new and should be captured
		// This includes: files, symlinks, and directories (even empty ones)
		return true
	}

	// Directory with new entries has modifications (children were added)
	if n.entries != nil && len(n.entries) > 0 {
		return true
	}

	// Directory with deleted entries has modifications
	if n.deletedEntries != nil && len(n.deletedEntries) > 0 {
		return true
	}

	// Check if xattrs have been modified
	if n.xattr != nil && len(n.xattr) > 0 {
		return true
	}

	return false
}

// buildNodePath constructs the full path for a node.
func (v *virtioFsBackend) buildNodePath(node *fsNode) string {
	if node.id == virtioFsRootNodeID {
		return ""
	}

	var parts []string
	current := node
	for current != nil && current.id != virtioFsRootNodeID {
		if current.name != "" {
			parts = append([]string{current.name}, parts...)
		}
		if current.parent == 0 || current.parent == virtioFsRootNodeID {
			break
		}
		current = v.nodes[current.parent]
	}

	return "/" + strings.Join(parts, "/")
}

// nodeToLayerEntry converts an fsNode to a LayerEntry.
func (v *virtioFsBackend) nodeToLayerEntry(node *fsNode, nodePath string) (fslayer.LayerEntry, error) {
	entry := fslayer.LayerEntry{
		Path:    nodePath,
		Mode:    node.mode,
		UID:     int(node.uid),
		GID:     int(node.gid),
		ModTime: node.modTime,
	}

	switch {
	case node.isDir():
		entry.Kind = fslayer.LayerEntryDirectory

	case node.isSymlink():
		entry.Kind = fslayer.LayerEntrySymlink
		entry.Data = []byte(node.symlinkTarget)

	default:
		entry.Kind = fslayer.LayerEntryRegular
		// Read file content
		data, err := v.readFullFile(node)
		if err != nil {
			return entry, err
		}
		entry.Data = data
		entry.Size = int64(len(data))
	}

	return entry, nil
}

// readFullFile reads all content from a file node.
func (v *virtioFsBackend) readFullFile(node *fsNode) ([]byte, error) {
	var size uint64
	if node.abstractFile != nil {
		size, _ = node.abstractFile.Stat()
	} else {
		size = node.size
	}

	if size == 0 {
		return nil, nil
	}

	// Use existing read method
	data, err := node.read(0, uint32(size))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// shouldExclude checks if a path should be excluded based on patterns.
func shouldExclude(nodePath string, excludes []string) bool {
	for _, pattern := range excludes {
		// Exact match
		if nodePath == pattern {
			return true
		}

		// Glob-style matching
		matched, err := path.Match(pattern, nodePath)
		if err == nil && matched {
			return true
		}

		// Also try matching just the base name
		matched, err = path.Match(pattern, path.Base(nodePath))
		if err == nil && matched {
			return true
		}

		// Check if pattern matches a parent directory
		if strings.HasPrefix(nodePath, pattern+"/") {
			return true
		}
	}
	return false
}

// ExportedNode contains node information for external access (testing).
type ExportedNode struct {
	ID            uint64
	Name          string
	Parent        uint64
	Mode          fs.FileMode
	Size          uint64
	IsModified    bool
	HasBlocks     bool
	HasAbstract   bool
	SymlinkTarget string
}

// ExportNodes returns all nodes for inspection (testing/debugging).
func (v *virtioFsBackend) ExportNodes() []ExportedNode {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ensureRoot()

	var nodes []ExportedNode
	for _, n := range v.nodes {
		nodes = append(nodes, ExportedNode{
			ID:            n.id,
			Name:          n.name,
			Parent:        n.parent,
			Mode:          n.mode,
			Size:          n.size,
			IsModified:    n.isModified(),
			HasBlocks:     n.blocks != nil && len(n.blocks) > 0,
			HasAbstract:   n.abstractFile != nil || n.abstractDir != nil,
			SymlinkTarget: n.symlinkTarget,
		})
	}
	return nodes
}
