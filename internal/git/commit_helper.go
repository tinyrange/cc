package git

import (
	"fmt"
	"time"
)

// CommitOptions contains options for creating a commit.
type CommitOptions struct {
	Author    Signature
	Committer Signature
	Message   string
	Parents   []Hash
}

// CreateCommit creates a new commit with the given tree and options.
// It writes the commit object and optionally updates a ref.
func (r *Repository) CreateCommit(treeHash Hash, opts CommitOptions) (Hash, error) {
	commit := &Commit{
		TreeHash:  treeHash,
		Parents:   opts.Parents,
		Author:    opts.Author,
		Committer: opts.Committer,
		Message:   opts.Message,
	}

	return r.WriteCommit(commit)
}

// CommitOnBranch creates a commit and updates the branch ref.
// If branch is empty, it updates HEAD directly (detached HEAD mode).
func (r *Repository) CommitOnBranch(treeHash Hash, branch string, opts CommitOptions) (Hash, error) {
	hash, err := r.CreateCommit(treeHash, opts)
	if err != nil {
		return ZeroHash, fmt.Errorf("create commit: %w", err)
	}

	if branch != "" {
		if err := r.UpdateRef(branch, hash); err != nil {
			return ZeroHash, fmt.Errorf("update ref: %w", err)
		}
	}

	return hash, nil
}

// TreeFromMap creates a tree from a map of path to file content.
// This is a convenience function for creating simple trees.
// All files are created with mode 0o100644 (non-executable).
func (r *Repository) TreeFromMap(files map[string][]byte) (Hash, error) {
	// Build a nested structure
	type dirEntry struct {
		files   map[string]Hash // leaf files
		subdirs map[string]*dirEntry
	}

	root := &dirEntry{
		files:   make(map[string]Hash),
		subdirs: make(map[string]*dirEntry),
	}

	// First, write all blobs and organize them into the tree structure
	for path, content := range files {
		blobHash, err := r.WriteBlob(content)
		if err != nil {
			return ZeroHash, fmt.Errorf("write blob for %s: %w", path, err)
		}

		parts := splitPath(path)
		current := root
		for i := 0; i < len(parts)-1; i++ {
			dir := parts[i]
			if current.subdirs[dir] == nil {
				current.subdirs[dir] = &dirEntry{
					files:   make(map[string]Hash),
					subdirs: make(map[string]*dirEntry),
				}
			}
			current = current.subdirs[dir]
		}
		current.files[parts[len(parts)-1]] = blobHash
	}

	// Now recursively build trees
	var buildTree func(entry *dirEntry) (Hash, error)
	buildTree = func(entry *dirEntry) (Hash, error) {
		builder := NewTreeBuilder()

		for name, hash := range entry.files {
			builder.AddBlob(name, hash, false)
		}

		for name, subdir := range entry.subdirs {
			subdirHash, err := buildTree(subdir)
			if err != nil {
				return ZeroHash, err
			}
			builder.AddTree(name, subdirHash)
		}

		tree := builder.Build()
		return r.WriteTree(tree)
	}

	return buildTree(root)
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// DefaultSignature creates a signature with a default name and email.
func DefaultSignature() Signature {
	return Signature{
		Name:  "Test User",
		Email: "test@example.com",
		When:  time.Now(),
	}
}

// SignatureAt creates a signature with a specific time.
func SignatureAt(name, email string, when time.Time) Signature {
	return Signature{
		Name:  name,
		Email: email,
		When:  when,
	}
}
