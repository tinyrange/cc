package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repository represents a local git repository.
type Repository struct {
	// Path is the path to the repository root (containing .git).
	Path string

	// GitDir is the path to the .git directory.
	GitDir string
}

// Open opens an existing git repository at the given path.
// It searches for a .git directory in the given path or any parent directory.
func Open(path string) (*Repository, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Walk up the directory tree looking for .git
	current := absPath
	for {
		gitDir := filepath.Join(current, ".git")
		info, err := os.Stat(gitDir)
		if err == nil {
			if info.IsDir() {
				return &Repository{
					Path:   current,
					GitDir: gitDir,
				}, nil
			}
			// Could be a gitdir file (for worktrees), but we don't support that yet
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached root without finding .git
			return nil, ErrNotARepository
		}
		current = parent
	}
}

// OpenAt opens a git repository at exactly the given path.
// Unlike Open, it does not search parent directories.
func OpenAt(path string) (*Repository, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	gitDir := filepath.Join(absPath, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotARepository
		}
		return nil, fmt.Errorf("stat .git: %w", err)
	}
	if !info.IsDir() {
		return nil, ErrNotARepository
	}

	return &Repository{
		Path:   absPath,
		GitDir: gitDir,
	}, nil
}

// objectPath returns the filesystem path for an object with the given hash.
func (r *Repository) objectPath(hash Hash) string {
	hashStr := hash.String()
	return filepath.Join(r.GitDir, "objects", hashStr[:2], hashStr[2:])
}

// ReadObject reads an object by its hash.
func (r *Repository) ReadObject(hash Hash) (*Object, error) {
	path := r.objectPath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read object file: %w", err)
	}
	return DecodeObject(data)
}

// WriteObject writes an object to the repository and returns its hash.
func (r *Repository) WriteObject(obj *Object) (Hash, error) {
	hash := obj.Hash()
	path := r.objectPath(hash)

	// Check if object already exists
	if _, err := os.Stat(path); err == nil {
		return hash, nil
	}

	// Create the directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ZeroHash, fmt.Errorf("create object directory: %w", err)
	}

	// Encode and write
	encoded, err := obj.Encode()
	if err != nil {
		return ZeroHash, fmt.Errorf("encode object: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o444); err != nil {
		return ZeroHash, fmt.Errorf("write object file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return ZeroHash, fmt.Errorf("rename object file: %w", err)
	}

	return hash, nil
}

// WriteBlob writes a blob to the repository.
func (r *Repository) WriteBlob(data []byte) (Hash, error) {
	blob := &Blob{Data: data}
	return r.WriteObject(blob.ToObject())
}

// WriteTree writes a tree to the repository.
func (r *Repository) WriteTree(tree *Tree) (Hash, error) {
	return r.WriteObject(tree.ToObject())
}

// WriteCommit writes a commit to the repository.
func (r *Repository) WriteCommit(commit *Commit) (Hash, error) {
	return r.WriteObject(commit.ToObject())
}

// ReadBlob reads a blob by its hash.
func (r *Repository) ReadBlob(hash Hash) (*Blob, error) {
	obj, err := r.ReadObject(hash)
	if err != nil {
		return nil, err
	}
	return ParseBlob(obj)
}

// ReadTree reads a tree by its hash.
func (r *Repository) ReadTree(hash Hash) (*Tree, error) {
	obj, err := r.ReadObject(hash)
	if err != nil {
		return nil, err
	}
	return ParseTree(obj)
}

// ReadCommit reads a commit by its hash.
func (r *Repository) ReadCommit(hash Hash) (*Commit, error) {
	obj, err := r.ReadObject(hash)
	if err != nil {
		return nil, err
	}
	return ParseCommit(obj)
}

// ResolveRef resolves a reference to a hash.
// Supports both full refs (refs/heads/main) and short refs (HEAD, main).
func (r *Repository) ResolveRef(ref string) (Hash, error) {
	// Try as a direct ref file first
	refPaths := []string{
		filepath.Join(r.GitDir, ref),
		filepath.Join(r.GitDir, "refs", "heads", ref),
		filepath.Join(r.GitDir, "refs", "tags", ref),
		filepath.Join(r.GitDir, "refs", "remotes", ref),
	}

	for _, refPath := range refPaths {
		content, err := os.ReadFile(refPath)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(content))

		// Check if it's a symbolic ref (prevent cycles by limiting depth)
		if strings.HasPrefix(line, "ref: ") {
			// TODO: Add cycle detection or depth limit
			return r.ResolveRef(strings.TrimPrefix(line, "ref: "))
		}

		// Try to parse as a hash
		return ParseHash(line)
	}

	// Maybe it's already a hash
	if len(ref) == 40 {
		return ParseHash(ref)
	}

	return ZeroHash, ErrInvalidRef
}

// UpdateRef updates a reference to point to the given hash.
func (r *Repository) UpdateRef(ref string, hash Hash) error {
	var refPath string
	if strings.HasPrefix(ref, "refs/") {
		refPath = filepath.Join(r.GitDir, ref)
	} else if ref == "HEAD" {
		refPath = filepath.Join(r.GitDir, "HEAD")
	} else {
		refPath = filepath.Join(r.GitDir, "refs", "heads", ref)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		return fmt.Errorf("create ref directory: %w", err)
	}

	content := hash.String() + "\n"
	return os.WriteFile(refPath, []byte(content), 0o644)
}

// Head returns the hash of HEAD.
func (r *Repository) Head() (Hash, error) {
	return r.ResolveRef("HEAD")
}

// GetHeadRef returns the reference that HEAD points to (e.g., "refs/heads/main").
// If HEAD is detached, it returns an empty string and the hash.
func (r *Repository) GetHeadRef() (string, Hash, error) {
	headPath := filepath.Join(r.GitDir, "HEAD")
	content, err := os.ReadFile(headPath)
	if err != nil {
		return "", ZeroHash, fmt.Errorf("read HEAD: %w", err)
	}

	line := strings.TrimSpace(string(content))
	if strings.HasPrefix(line, "ref: ") {
		ref := strings.TrimPrefix(line, "ref: ")
		hash, err := r.ResolveRef(ref)
		if err != nil {
			// Ref might not exist yet (empty repo)
			return ref, ZeroHash, nil
		}
		return ref, hash, nil
	}

	// Detached HEAD
	hash, err := ParseHash(line)
	return "", hash, err
}

// ReadFileAtCommit reads a file from the repository at a given commit.
func (r *Repository) ReadFileAtCommit(commitHash Hash, path string) ([]byte, error) {
	commit, err := r.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}

	return r.ReadFileAtTree(commit.TreeHash, path)
}

// ReadFileAtTree reads a file from a tree, following the path.
func (r *Repository) ReadFileAtTree(treeHash Hash, path string) ([]byte, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")

	currentHash := treeHash
	depth := 0
	for i, part := range parts {
		if part == "" {
			continue
		}

		depth++
		if depth > maxTreeDepth {
			return nil, fmt.Errorf("maximum tree depth exceeded")
		}

		tree, err := r.ReadTree(currentHash)
		if err != nil {
			return nil, fmt.Errorf("read tree: %w", err)
		}

		var found bool
		for _, entry := range tree.Entries {
			if entry.Name == part {
				if i == len(parts)-1 {
					// This is the file
					blob, err := r.ReadBlob(entry.Hash)
					if err != nil {
						return nil, fmt.Errorf("read blob: %w", err)
					}
					return blob.Data, nil
				}
				// This is a directory, continue
				if !entry.IsDir() {
					return nil, fmt.Errorf("%s is not a directory", part)
				}
				currentHash = entry.Hash
				found = true
				break
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}

	return nil, ErrNotFound
}

// ListFilesAtTree lists all files in a tree recursively.
func (r *Repository) ListFilesAtTree(treeHash Hash, prefix string) ([]string, error) {
	return r.listFilesAtTreeDepth(treeHash, prefix, 0)
}

const maxTreeDepth = 100

func (r *Repository) listFilesAtTreeDepth(treeHash Hash, prefix string, depth int) ([]string, error) {
	if depth > maxTreeDepth {
		return nil, fmt.Errorf("maximum tree depth exceeded")
	}

	tree, err := r.ReadTree(treeHash)
	if err != nil {
		return nil, fmt.Errorf("read tree: %w", err)
	}

	var files []string
	for _, entry := range tree.Entries {
		path := entry.Name
		if prefix != "" {
			path = prefix + "/" + path
		}

		if entry.IsDir() {
			subFiles, err := r.listFilesAtTreeDepth(entry.Hash, path, depth+1)
			if err != nil {
				return nil, err
			}
			files = append(files, subFiles...)
		} else {
			files = append(files, path)
		}
	}

	return files, nil
}

// ListFilesAtCommit lists all files at a given commit.
func (r *Repository) ListFilesAtCommit(commitHash Hash) ([]string, error) {
	commit, err := r.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}

	return r.ListFilesAtTree(commit.TreeHash, "")
}
