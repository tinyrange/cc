// Package git provides functionality for reading and writing git repositories.
package git

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ObjectType represents the type of a git object.
type ObjectType string

const (
	ObjectTypeBlob   ObjectType = "blob"
	ObjectTypeTree   ObjectType = "tree"
	ObjectTypeCommit ObjectType = "commit"
	ObjectTypeTag    ObjectType = "tag"
)

// Common errors.
var (
	ErrNotFound       = errors.New("object not found")
	ErrInvalidObject  = errors.New("invalid object format")
	ErrNotARepository = errors.New("not a git repository")
	ErrInvalidRef     = errors.New("invalid reference")
)

// Hash represents a git object hash (SHA-1, 20 bytes).
type Hash [20]byte

// ZeroHash is the zero-value hash.
var ZeroHash Hash

// String returns the hex-encoded hash.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// IsZero returns true if the hash is all zeros.
func (h Hash) IsZero() bool {
	return h == ZeroHash
}

// ParseHash parses a hex-encoded hash string.
func ParseHash(s string) (Hash, error) {
	var h Hash
	if len(s) != 40 {
		return h, fmt.Errorf("invalid hash length: %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("invalid hash: %w", err)
	}
	copy(h[:], b)
	return h, nil
}

// Object represents a git object.
type Object struct {
	Type ObjectType
	Data []byte
}

// Hash computes the SHA-1 hash of the object.
func (o *Object) Hash() Hash {
	header := fmt.Sprintf("%s %d\x00", o.Type, len(o.Data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(o.Data)
	var result Hash
	copy(result[:], h.Sum(nil))
	return result
}

// Encode returns the compressed git object data.
func (o *Object) Encode() ([]byte, error) {
	header := fmt.Sprintf("%s %d\x00", o.Type, len(o.Data))
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write([]byte(header)); err != nil {
		return nil, err
	}
	if _, err := w.Write(o.Data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeObject parses a compressed git object.
func DecodeObject(data []byte) (*Object, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("zlib decompress: %w", err)
	}
	defer r.Close()

	// Limit decompressed size to prevent decompression bombs (e.g., 100MB)
	const maxDecompressedSize = 100 * 1024 * 1024
	lr := io.LimitReader(r, maxDecompressedSize+1)
	decompressed, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read decompressed data: %w", err)
	}
	if len(decompressed) > maxDecompressedSize {
		return nil, fmt.Errorf("decompressed object too large")
	}

	// Find the null byte separating header from content
	nullIdx := bytes.IndexByte(decompressed, 0)
	if nullIdx == -1 {
		return nil, ErrInvalidObject
	}

	header := string(decompressed[:nullIdx])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidObject
	}

	objType := ObjectType(parts[0])
	size, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid size: %w", err)
	}

	content := decompressed[nullIdx+1:]
	if len(content) != size {
		return nil, fmt.Errorf("size mismatch: header says %d, got %d", size, len(content))
	}

	return &Object{
		Type: objType,
		Data: content,
	}, nil
}

// Blob represents a git blob (file content).
type Blob struct {
	Data []byte
}

// ToObject converts a Blob to an Object.
func (b *Blob) ToObject() *Object {
	return &Object{
		Type: ObjectTypeBlob,
		Data: b.Data,
	}
}

// ParseBlob parses a blob from an Object.
func ParseBlob(obj *Object) (*Blob, error) {
	if obj.Type != ObjectTypeBlob {
		return nil, fmt.Errorf("expected blob, got %s", obj.Type)
	}
	return &Blob{Data: obj.Data}, nil
}

// TreeEntry represents a single entry in a tree.
type TreeEntry struct {
	Mode uint32
	Name string
	Hash Hash
}

// IsDir returns true if the entry is a directory (tree).
func (e *TreeEntry) IsDir() bool {
	return e.Mode == 0o40000
}

// Tree represents a git tree (directory listing).
type Tree struct {
	Entries []TreeEntry
}

// ToObject converts a Tree to an Object.
func (t *Tree) ToObject() *Object {
	var buf bytes.Buffer
	for _, entry := range t.Entries {
		// Git uses octal mode without leading zeros (except for dirs)
		mode := fmt.Sprintf("%o", entry.Mode)
		buf.WriteString(mode)
		buf.WriteByte(' ')
		buf.WriteString(entry.Name)
		buf.WriteByte(0)
		buf.Write(entry.Hash[:])
	}
	return &Object{
		Type: ObjectTypeTree,
		Data: buf.Bytes(),
	}
}

// ParseTree parses a tree from an Object.
func ParseTree(obj *Object) (*Tree, error) {
	if obj.Type != ObjectTypeTree {
		return nil, fmt.Errorf("expected tree, got %s", obj.Type)
	}

	var entries []TreeEntry
	data := obj.Data
	for len(data) > 0 {
		// Find space separating mode from name
		spaceIdx := bytes.IndexByte(data, ' ')
		if spaceIdx == -1 {
			return nil, ErrInvalidObject
		}
		modeStr := string(data[:spaceIdx])
		mode, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid mode %q: %w", modeStr, err)
		}
		data = data[spaceIdx+1:]

		// Find null byte after name
		nullIdx := bytes.IndexByte(data, 0)
		if nullIdx == -1 {
			return nil, ErrInvalidObject
		}
		name := string(data[:nullIdx])
		data = data[nullIdx+1:]

		// Read 20-byte hash
		if len(data) < 20 {
			return nil, ErrInvalidObject
		}
		var hash Hash
		copy(hash[:], data[:20])
		data = data[20:]

		entries = append(entries, TreeEntry{
			Mode: uint32(mode),
			Name: name,
			Hash: hash,
		})
	}

	return &Tree{Entries: entries}, nil
}

// Signature represents the author or committer of a commit.
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// String returns the signature in git format.
func (s *Signature) String() string {
	return fmt.Sprintf("%s <%s> %d %s",
		s.Name, s.Email,
		s.When.Unix(),
		s.When.Format("-0700"))
}

// ParseSignature parses a signature from git format.
func ParseSignature(line string) (*Signature, error) {
	// Format: "Name <email> timestamp timezone"
	ltIdx := strings.LastIndex(line, " <")
	if ltIdx == -1 {
		return nil, fmt.Errorf("invalid signature: no '<' found")
	}

	gtIdx := strings.Index(line[ltIdx:], ">")
	if gtIdx == -1 {
		return nil, fmt.Errorf("invalid signature: no '>' found")
	}
	gtIdx += ltIdx

	name := line[:ltIdx]
	email := line[ltIdx+2 : gtIdx]
	rest := strings.TrimSpace(line[gtIdx+1:])

	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid signature: missing timestamp or timezone")
	}

	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %w", err)
	}

	// Parse timezone offset
	tz := parts[1]
	loc := time.FixedZone(tz, parseTimezone(tz))

	return &Signature{
		Name:  name,
		Email: email,
		When:  time.Unix(timestamp, 0).In(loc),
	}, nil
}

// parseTimezone parses a git timezone string (e.g., "+0530", "-0800") to seconds offset.
// Returns 0 (UTC) for malformed input - this is intentional to match git's lenient
// parsing behavior and allow reading commits with unusual or corrupted timezone data.
func parseTimezone(tz string) int {
	if len(tz) != 5 {
		return 0
	}
	sign := 1
	if tz[0] == '-' {
		sign = -1
	}
	hours, _ := strconv.Atoi(tz[1:3])
	minutes, _ := strconv.Atoi(tz[3:5])
	return sign * (hours*3600 + minutes*60)
}

// Commit represents a git commit.
type Commit struct {
	TreeHash  Hash
	Parents   []Hash
	Author    Signature
	Committer Signature
	Message   string
}

// ToObject converts a Commit to an Object.
func (c *Commit) ToObject() *Object {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("tree %s\n", c.TreeHash))
	for _, parent := range c.Parents {
		buf.WriteString(fmt.Sprintf("parent %s\n", parent))
	}
	buf.WriteString(fmt.Sprintf("author %s\n", c.Author.String()))
	buf.WriteString(fmt.Sprintf("committer %s\n", c.Committer.String()))
	buf.WriteByte('\n')
	buf.WriteString(c.Message)
	return &Object{
		Type: ObjectTypeCommit,
		Data: buf.Bytes(),
	}
}

// ParseCommit parses a commit from an Object.
func ParseCommit(obj *Object) (*Commit, error) {
	if obj.Type != ObjectTypeCommit {
		return nil, fmt.Errorf("expected commit, got %s", obj.Type)
	}

	commit := &Commit{}
	data := string(obj.Data)

	// Split header and message (message may be empty)
	parts := strings.SplitN(data, "\n\n", 2)
	if len(parts) == 2 {
		commit.Message = parts[1]
	}
	headers := parts[0]

	headers := parts[0]
	commit.Message = parts[1]

	for _, line := range strings.Split(headers, "\n") {
		if line == "" {
			continue
		}
		spaceIdx := strings.Index(line, " ")
		if spaceIdx == -1 {
			continue
		}
		key := line[:spaceIdx]
		value := line[spaceIdx+1:]

		switch key {
		case "tree":
			hash, err := ParseHash(value)
			if err != nil {
				return nil, fmt.Errorf("invalid tree hash: %w", err)
			}
			commit.TreeHash = hash
		case "parent":
			hash, err := ParseHash(value)
			if err != nil {
				return nil, fmt.Errorf("invalid parent hash: %w", err)
			}
			commit.Parents = append(commit.Parents, hash)
		case "author":
			sig, err := ParseSignature(value)
			if err != nil {
				return nil, fmt.Errorf("invalid author: %w", err)
			}
			commit.Author = *sig
		case "committer":
			sig, err := ParseSignature(value)
			if err != nil {
				return nil, fmt.Errorf("invalid committer: %w", err)
			}
			commit.Committer = *sig
		}
	}

	return commit, nil
}

// TreeBuilder helps construct tree objects.
type TreeBuilder struct {
	entries []TreeEntry
}

// NewTreeBuilder creates a new TreeBuilder.
func NewTreeBuilder() *TreeBuilder {
	return &TreeBuilder{}
}

// AddBlob adds a blob entry to the tree.
func (tb *TreeBuilder) AddBlob(name string, hash Hash, executable bool) {
	mode := uint32(0o100644)
	if executable {
		mode = 0o100755
	}
	tb.entries = append(tb.entries, TreeEntry{
		Mode: mode,
		Name: name,
		Hash: hash,
	})
}

// AddTree adds a subtree entry.
func (tb *TreeBuilder) AddTree(name string, hash Hash) {
	tb.entries = append(tb.entries, TreeEntry{
		Mode: 0o40000,
		Name: name,
		Hash: hash,
	})
}

// AddSymlink adds a symbolic link entry.
func (tb *TreeBuilder) AddSymlink(name string, hash Hash) {
	tb.entries = append(tb.entries, TreeEntry{
		Mode: 0o120000,
		Name: name,
		Hash: hash,
	})
}

// Build creates the Tree, sorting entries as git does.
func (tb *TreeBuilder) Build() *Tree {
	// Git sorts tree entries by name, with directories having a trailing /
	sorted := make([]TreeEntry, len(tb.entries))
	copy(sorted, tb.entries)
	sort.Slice(sorted, func(i, j int) bool {
		nameI := sorted[i].Name
		nameJ := sorted[j].Name
		if sorted[i].IsDir() {
			nameI += "/"
		}
		if sorted[j].IsDir() {
			nameJ += "/"
		}
		return nameI < nameJ
	})
	return &Tree{Entries: sorted}
}
