package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testRepo creates a temporary git repository for testing.
// It returns the repository and a cleanup function.
func testRepo(t *testing.T) (*Repository, func()) {
	t.Helper()

	dir := t.TempDir()

	// Initialize a git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Configure user and disable signing
	for _, args := range [][]string{
		{"config", "user.name", "Test User"},
		{"config", "user.email", "test@example.com"},
		{"config", "commit.gpgsign", "false"},
		{"config", "tag.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config failed: %v\n%s", err, out)
		}
	}

	repo, err := OpenAt(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}

	return repo, func() {
		// TempDir cleanup is automatic
	}
}

// gitCommand runs a git command in the repository directory.
func gitCommand(t *testing.T, repo *Repository, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo.Path
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestOpenRepository(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Verify we can open the repo
	if repo.GitDir != filepath.Join(repo.Path, ".git") {
		t.Errorf("unexpected GitDir: %s", repo.GitDir)
	}

	// Test Open with subdirectory
	subdir := filepath.Join(repo.Path, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	subRepo, err := Open(subdir)
	if err != nil {
		t.Fatalf("Open from subdir: %v", err)
	}
	if subRepo.Path != repo.Path {
		t.Errorf("expected root %s, got %s", repo.Path, subRepo.Path)
	}
}

func TestOpenNotARepository(t *testing.T) {
	dir := t.TempDir()

	_, err := OpenAt(dir)
	if err != ErrNotARepository {
		t.Errorf("expected ErrNotARepository, got %v", err)
	}
}

func TestWriteAndReadBlob(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	content := []byte("Hello, World!\n")
	hash, err := repo.WriteBlob(content)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	// Verify with git cat-file
	cmd := exec.Command("git", "hash-object", "-t", "blob", "--stdin")
	cmd.Dir = repo.Path
	cmd.Stdin = strings.NewReader(string(content))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git hash-object: %v", err)
	}
	expectedHash := strings.TrimSpace(string(out))

	if hash.String() != expectedHash {
		t.Errorf("hash mismatch: got %s, expected %s", hash, expectedHash)
	}

	// Read it back
	blob, err := repo.ReadBlob(hash)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(blob.Data) != string(content) {
		t.Errorf("content mismatch: got %q, expected %q", blob.Data, content)
	}

	// Verify git can read our object
	gitContent := gitCommand(t, repo, "cat-file", "-p", hash.String())
	if gitContent != strings.TrimSpace(string(content)) {
		t.Errorf("git cat-file content mismatch: got %q, expected %q", gitContent, content)
	}
}

func TestWriteAndReadTree(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create a blob first
	blobContent := []byte("file content\n")
	blobHash, err := repo.WriteBlob(blobContent)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	// Create a tree with the blob
	builder := NewTreeBuilder()
	builder.AddBlob("file.txt", blobHash, false)
	tree := builder.Build()

	treeHash, err := repo.WriteTree(tree)
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	// Verify with git ls-tree
	lsOutput := gitCommand(t, repo, "ls-tree", treeHash.String())
	expectedLs := "100644 blob " + blobHash.String() + "\tfile.txt"
	if lsOutput != expectedLs {
		t.Errorf("ls-tree mismatch:\ngot:      %s\nexpected: %s", lsOutput, expectedLs)
	}

	// Read it back
	readTree, err := repo.ReadTree(treeHash)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(readTree.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(readTree.Entries))
	}
	if readTree.Entries[0].Name != "file.txt" {
		t.Errorf("unexpected entry name: %s", readTree.Entries[0].Name)
	}
	if readTree.Entries[0].Hash != blobHash {
		t.Errorf("entry hash mismatch")
	}
}

func TestWriteAndReadCommit(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create a tree
	blobHash, _ := repo.WriteBlob([]byte("test content\n"))
	builder := NewTreeBuilder()
	builder.AddBlob("test.txt", blobHash, false)
	treeHash, _ := repo.WriteTree(builder.Build())

	// Create a commit
	when := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	commit := &Commit{
		TreeHash: treeHash,
		Author: Signature{
			Name:  "Test Author",
			Email: "author@example.com",
			When:  when,
		},
		Committer: Signature{
			Name:  "Test Committer",
			Email: "committer@example.com",
			When:  when,
		},
		Message: "Initial commit\n",
	}

	commitHash, err := repo.WriteCommit(commit)
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	// Verify with git cat-file
	gitCommit := gitCommand(t, repo, "cat-file", "-p", commitHash.String())
	if !strings.Contains(gitCommit, "tree "+treeHash.String()) {
		t.Errorf("commit missing tree line: %s", gitCommit)
	}
	if !strings.Contains(gitCommit, "author Test Author <author@example.com>") {
		t.Errorf("commit missing author: %s", gitCommit)
	}

	// Read it back
	readCommit, err := repo.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if readCommit.TreeHash != treeHash {
		t.Errorf("tree hash mismatch")
	}
	if readCommit.Author.Name != "Test Author" {
		t.Errorf("author name mismatch: %s", readCommit.Author.Name)
	}
	if readCommit.Message != "Initial commit\n" {
		t.Errorf("message mismatch: %q", readCommit.Message)
	}
}

func TestCommitWithParent(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create first commit
	blob1Hash, _ := repo.WriteBlob([]byte("first\n"))
	builder1 := NewTreeBuilder()
	builder1.AddBlob("file.txt", blob1Hash, false)
	tree1Hash, _ := repo.WriteTree(builder1.Build())

	when := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	sig := Signature{Name: "Test", Email: "test@example.com", When: when}

	commit1 := &Commit{
		TreeHash:  tree1Hash,
		Author:    sig,
		Committer: sig,
		Message:   "First commit\n",
	}
	commit1Hash, _ := repo.WriteCommit(commit1)

	// Create second commit with parent
	blob2Hash, _ := repo.WriteBlob([]byte("second\n"))
	builder2 := NewTreeBuilder()
	builder2.AddBlob("file.txt", blob2Hash, false)
	tree2Hash, _ := repo.WriteTree(builder2.Build())

	commit2 := &Commit{
		TreeHash:  tree2Hash,
		Parents:   []Hash{commit1Hash},
		Author:    sig,
		Committer: sig,
		Message:   "Second commit\n",
	}
	commit2Hash, _ := repo.WriteCommit(commit2)

	// Verify with git
	gitCommit := gitCommand(t, repo, "cat-file", "-p", commit2Hash.String())
	if !strings.Contains(gitCommit, "parent "+commit1Hash.String()) {
		t.Errorf("commit missing parent: %s", gitCommit)
	}

	// Read back and verify parent
	readCommit, _ := repo.ReadCommit(commit2Hash)
	if len(readCommit.Parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(readCommit.Parents))
	}
	if readCommit.Parents[0] != commit1Hash {
		t.Errorf("parent hash mismatch")
	}
}

func TestTreeFromMap(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	files := map[string][]byte{
		"README.md":       []byte("# Test\n"),
		"src/main.go":     []byte("package main\n"),
		"src/lib/util.go": []byte("package lib\n"),
	}

	treeHash, err := repo.TreeFromMap(files)
	if err != nil {
		t.Fatalf("TreeFromMap: %v", err)
	}

	// List files using git
	gitFiles := gitCommand(t, repo, "ls-tree", "-r", "--name-only", treeHash.String())
	expectedFiles := "README.md\nsrc/lib/util.go\nsrc/main.go"
	if gitFiles != expectedFiles {
		t.Errorf("file list mismatch:\ngot:      %s\nexpected: %s", gitFiles, expectedFiles)
	}

	// List files using our code
	listedFiles, err := repo.ListFilesAtTree(treeHash, "")
	if err != nil {
		t.Fatalf("ListFilesAtTree: %v", err)
	}

	if len(listedFiles) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(listedFiles), listedFiles)
	}
}

func TestReadFileAtTree(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	expectedContent := "package main\n\nfunc main() {}\n"
	files := map[string][]byte{
		"src/main.go": []byte(expectedContent),
	}

	treeHash, err := repo.TreeFromMap(files)
	if err != nil {
		t.Fatalf("TreeFromMap: %v", err)
	}

	// Read using our code
	content, err := repo.ReadFileAtTree(treeHash, "src/main.go")
	if err != nil {
		t.Fatalf("ReadFileAtTree: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("content mismatch: got %q, expected %q", content, expectedContent)
	}

	// Read non-existent file
	_, err = repo.ReadFileAtTree(treeHash, "nonexistent.txt")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveRef(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create a commit and update HEAD
	blobHash, _ := repo.WriteBlob([]byte("test\n"))
	builder := NewTreeBuilder()
	builder.AddBlob("test.txt", blobHash, false)
	treeHash, _ := repo.WriteTree(builder.Build())

	when := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	sig := Signature{Name: "Test", Email: "test@example.com", When: when}
	commit := &Commit{
		TreeHash:  treeHash,
		Author:    sig,
		Committer: sig,
		Message:   "Test commit\n",
	}
	commitHash, _ := repo.WriteCommit(commit)

	// Update refs/heads/main
	if err := repo.UpdateRef("refs/heads/main", commitHash); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	// Resolve by full ref
	resolved, err := repo.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef full: %v", err)
	}
	if resolved != commitHash {
		t.Errorf("hash mismatch: got %s, expected %s", resolved, commitHash)
	}

	// Resolve by short ref
	resolved, err = repo.ResolveRef("main")
	if err != nil {
		t.Fatalf("ResolveRef short: %v", err)
	}
	if resolved != commitHash {
		t.Errorf("hash mismatch: got %s, expected %s", resolved, commitHash)
	}
}

func TestReadExistingGitCommit(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Use git to create a commit
	testFile := filepath.Join(repo.Path, "test.txt")
	if err := os.WriteFile(testFile, []byte("Hello from git\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gitCommand(t, repo, "add", "test.txt")

	cmd := exec.Command("git", "commit", "-m", "Created by git")
	cmd.Dir = repo.Path
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Git Author",
		"GIT_AUTHOR_EMAIL=git@example.com",
		"GIT_COMMITTER_NAME=Git Committer",
		"GIT_COMMITTER_EMAIL=git@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// Get HEAD hash from git
	headHashStr := gitCommand(t, repo, "rev-parse", "HEAD")
	headHash, err := ParseHash(headHashStr)
	if err != nil {
		t.Fatalf("parse HEAD hash: %v", err)
	}

	// Read commit using our library
	commit, err := repo.ReadCommit(headHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}

	if commit.Message != "Created by git\n" {
		t.Errorf("unexpected message: %q", commit.Message)
	}
	if commit.Author.Name != "Git Author" {
		t.Errorf("unexpected author: %s", commit.Author.Name)
	}

	// Read file content
	content, err := repo.ReadFileAtCommit(headHash, "test.txt")
	if err != nil {
		t.Fatalf("ReadFileAtCommit: %v", err)
	}
	if string(content) != "Hello from git\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestGitCanReadOurCommit(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create a commit using our library
	files := map[string][]byte{
		"main.go":     []byte("package main\n\nfunc main() {}\n"),
		"lib/util.go": []byte("package lib\n"),
	}

	treeHash, err := repo.TreeFromMap(files)
	if err != nil {
		t.Fatalf("TreeFromMap: %v", err)
	}

	when := time.Date(2024, 6, 15, 14, 30, 0, 0, time.FixedZone("", -7*3600))
	opts := CommitOptions{
		Author:    SignatureAt("Our Author", "our@example.com", when),
		Committer: SignatureAt("Our Committer", "our@example.com", when),
		Message:   "Commit created by our library\n",
	}

	commitHash, err := repo.CommitOnBranch(treeHash, "refs/heads/main", opts)
	if err != nil {
		t.Fatalf("CommitOnBranch: %v", err)
	}

	// Verify git can read it
	gitLog := gitCommand(t, repo, "log", "--oneline", "-1", commitHash.String())
	if !strings.Contains(gitLog, "Commit created by our library") {
		t.Errorf("git log unexpected: %s", gitLog)
	}

	// Verify file contents via git show
	gitShow := gitCommand(t, repo, "show", commitHash.String()+":main.go")
	if gitShow != "package main\n\nfunc main() {}" {
		t.Errorf("git show unexpected: %q", gitShow)
	}

	// Verify we can checkout
	gitCommand(t, repo, "checkout", commitHash.String())
	content, err := os.ReadFile(filepath.Join(repo.Path, "main.go"))
	if err != nil {
		t.Fatalf("read checked out file: %v", err)
	}
	if string(content) != "package main\n\nfunc main() {}\n" {
		t.Errorf("checked out content mismatch: %q", content)
	}
}

func TestTreeEntryModes(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create blobs
	normalBlob, _ := repo.WriteBlob([]byte("normal file\n"))
	execBlob, _ := repo.WriteBlob([]byte("#!/bin/sh\necho hi\n"))
	linkBlob, _ := repo.WriteBlob([]byte("target.txt"))

	// Build tree with different modes
	builder := NewTreeBuilder()
	builder.AddBlob("normal.txt", normalBlob, false)
	builder.AddBlob("script.sh", execBlob, true)
	builder.AddSymlink("link.txt", linkBlob)

	tree := builder.Build()
	treeHash, _ := repo.WriteTree(tree)

	// Verify with git
	lsOutput := gitCommand(t, repo, "ls-tree", treeHash.String())
	lines := strings.Split(lsOutput, "\n")

	modeMap := make(map[string]string)
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			// Extract filename from the tab-separated part
			namePart := strings.SplitN(line, "\t", 2)
			if len(namePart) == 2 {
				modeMap[namePart[1]] = parts[0]
			}
		}
	}

	if modeMap["normal.txt"] != "100644" {
		t.Errorf("normal.txt mode: %s", modeMap["normal.txt"])
	}
	if modeMap["script.sh"] != "100755" {
		t.Errorf("script.sh mode: %s", modeMap["script.sh"])
	}
	if modeMap["link.txt"] != "120000" {
		t.Errorf("link.txt mode: %s", modeMap["link.txt"])
	}
}

func TestNestedTree(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Create nested directory structure manually
	file1, _ := repo.WriteBlob([]byte("file1\n"))
	file2, _ := repo.WriteBlob([]byte("file2\n"))
	file3, _ := repo.WriteBlob([]byte("file3\n"))

	// Build inner tree
	innerBuilder := NewTreeBuilder()
	innerBuilder.AddBlob("inner.txt", file3, false)
	innerTree := innerBuilder.Build()
	innerHash, _ := repo.WriteTree(innerTree)

	// Build outer tree
	outerBuilder := NewTreeBuilder()
	outerBuilder.AddBlob("root.txt", file1, false)
	outerBuilder.AddTree("dir", innerHash)
	outerTree := outerBuilder.Build()
	outerHash, _ := repo.WriteTree(outerTree)

	// Now build top level
	topBuilder := NewTreeBuilder()
	topBuilder.AddBlob("top.txt", file2, false)
	topBuilder.AddTree("sub", outerHash)
	topTree := topBuilder.Build()
	topHash, _ := repo.WriteTree(topTree)

	// Verify with git
	gitFiles := gitCommand(t, repo, "ls-tree", "-r", "--name-only", topHash.String())
	expected := "sub/dir/inner.txt\nsub/root.txt\ntop.txt"
	if gitFiles != expected {
		t.Errorf("file list mismatch:\ngot:      %s\nexpected: %s", gitFiles, expected)
	}

	// Read deeply nested file
	content, err := repo.ReadFileAtTree(topHash, "sub/dir/inner.txt")
	if err != nil {
		t.Fatalf("ReadFileAtTree: %v", err)
	}
	if string(content) != "file3\n" {
		t.Errorf("content mismatch: %q", content)
	}
}

func TestHashParsing(t *testing.T) {
	// Valid hash
	hashStr := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	hash, err := ParseHash(hashStr)
	if err != nil {
		t.Fatalf("ParseHash: %v", err)
	}
	if hash.String() != hashStr {
		t.Errorf("hash mismatch: got %s, expected %s", hash, hashStr)
	}

	// Invalid length
	_, err = ParseHash("abc")
	if err == nil {
		t.Error("expected error for short hash")
	}

	// Invalid characters
	_, err = ParseHash("zz39a3ee5e6b4b0d3255bfef95601890afd80709")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestSignatureParsing(t *testing.T) {
	tests := []struct {
		input    string
		name     string
		email    string
		unixTime int64
	}{
		{
			"Test User <test@example.com> 1705320000 +0000",
			"Test User", "test@example.com", 1705320000,
		},
		{
			"John Doe <john@example.com> 1705320000 -0700",
			"John Doe", "john@example.com", 1705320000,
		},
	}

	for _, tc := range tests {
		sig, err := ParseSignature(tc.input)
		if err != nil {
			t.Errorf("ParseSignature(%q): %v", tc.input, err)
			continue
		}
		if sig.Name != tc.name {
			t.Errorf("name: got %q, expected %q", sig.Name, tc.name)
		}
		if sig.Email != tc.email {
			t.Errorf("email: got %q, expected %q", sig.Email, tc.email)
		}
		if sig.When.Unix() != tc.unixTime {
			t.Errorf("time: got %d, expected %d", sig.When.Unix(), tc.unixTime)
		}
	}
}

func TestObjectNotFound(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	// Try to read non-existent object
	hash, _ := ParseHash("da39a3ee5e6b4b0d3255bfef95601890afd80709")
	_, err := repo.ReadObject(hash)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestEmptyTreeSorting(t *testing.T) {
	// Verify that entries are sorted correctly
	builder := NewTreeBuilder()

	// Add in non-sorted order
	hash1, _ := ParseHash("0000000000000000000000000000000000000001")
	hash2, _ := ParseHash("0000000000000000000000000000000000000002")
	hash3, _ := ParseHash("0000000000000000000000000000000000000003")
	hash4, _ := ParseHash("0000000000000000000000000000000000000004")

	builder.AddBlob("zebra.txt", hash1, false)
	builder.AddTree("adir", hash2)
	builder.AddBlob("apple.txt", hash3, false)
	builder.AddTree("zdir", hash4)

	tree := builder.Build()

	// Git sorts with directories treated as having trailing /
	// So order should be: adir/, apple.txt, zdir/, zebra.txt
	expectedOrder := []string{"adir", "apple.txt", "zdir", "zebra.txt"}
	for i, entry := range tree.Entries {
		if entry.Name != expectedOrder[i] {
			t.Errorf("entry %d: got %s, expected %s", i, entry.Name, expectedOrder[i])
		}
	}
}

func TestMergeCommit(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()

	when := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	sig := Signature{Name: "Test", Email: "test@example.com", When: when}

	// Create two parent commits
	blob1, _ := repo.WriteBlob([]byte("branch1\n"))
	builder1 := NewTreeBuilder()
	builder1.AddBlob("file.txt", blob1, false)
	tree1, _ := repo.WriteTree(builder1.Build())
	parent1, _ := repo.WriteCommit(&Commit{
		TreeHash:  tree1,
		Author:    sig,
		Committer: sig,
		Message:   "Branch 1\n",
	})

	blob2, _ := repo.WriteBlob([]byte("branch2\n"))
	builder2 := NewTreeBuilder()
	builder2.AddBlob("file.txt", blob2, false)
	tree2, _ := repo.WriteTree(builder2.Build())
	parent2, _ := repo.WriteCommit(&Commit{
		TreeHash:  tree2,
		Author:    sig,
		Committer: sig,
		Message:   "Branch 2\n",
	})

	// Create merge commit
	mergeBlob, _ := repo.WriteBlob([]byte("merged\n"))
	mergeBuilder := NewTreeBuilder()
	mergeBuilder.AddBlob("file.txt", mergeBlob, false)
	mergeTree, _ := repo.WriteTree(mergeBuilder.Build())

	mergeCommit := &Commit{
		TreeHash:  mergeTree,
		Parents:   []Hash{parent1, parent2},
		Author:    sig,
		Committer: sig,
		Message:   "Merge commit\n",
	}
	mergeHash, _ := repo.WriteCommit(mergeCommit)

	// Verify with git
	gitCommit := gitCommand(t, repo, "cat-file", "-p", mergeHash.String())
	if !strings.Contains(gitCommit, "parent "+parent1.String()) {
		t.Errorf("missing parent 1")
	}
	if !strings.Contains(gitCommit, "parent "+parent2.String()) {
		t.Errorf("missing parent 2")
	}

	// Read back
	read, _ := repo.ReadCommit(mergeHash)
	if len(read.Parents) != 2 {
		t.Fatalf("expected 2 parents, got %d", len(read.Parents))
	}
}
