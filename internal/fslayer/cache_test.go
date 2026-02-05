package fslayer

import (
	"testing"
)

func TestDeriveKey(t *testing.T) {
	// Same inputs should produce same output
	key1 := DeriveKey("parent1", "op1")
	key2 := DeriveKey("parent1", "op1")
	if key1 != key2 {
		t.Errorf("DeriveKey not deterministic: %s != %s", key1, key2)
	}

	// Different inputs should produce different output
	key3 := DeriveKey("parent1", "op2")
	if key1 == key3 {
		t.Error("Different ops should produce different keys")
	}

	key4 := DeriveKey("parent2", "op1")
	if key1 == key4 {
		t.Error("Different parents should produce different keys")
	}

	// Key should be 32 characters (128 bits)
	if len(key1) != 32 {
		t.Errorf("Expected 32 char key, got %d", len(key1))
	}
}

func TestBaseKey(t *testing.T) {
	// Same inputs should produce same output
	key1 := BaseKey("alpine:latest", "amd64")
	key2 := BaseKey("alpine:latest", "amd64")
	if key1 != key2 {
		t.Errorf("BaseKey not deterministic: %s != %s", key1, key2)
	}

	// Different image should produce different key
	key3 := BaseKey("ubuntu:latest", "amd64")
	if key1 == key3 {
		t.Error("Different images should produce different keys")
	}

	// Different arch should produce different key
	key4 := BaseKey("alpine:latest", "arm64")
	if key1 == key4 {
		t.Error("Different architectures should produce different keys")
	}

	// Key should be 32 characters
	if len(key1) != 32 {
		t.Errorf("Expected 32 char key, got %d", len(key1))
	}
}

func TestSnapshotOpKey(t *testing.T) {
	key := SnapshotOpKey("abc123")
	if key != "snapshot:abc123" {
		t.Errorf("Unexpected snapshot op key: %s", key)
	}
}

func TestRunOpKey(t *testing.T) {
	key1 := RunOpKey([]string{"echo", "hello"}, []string{"FOO=bar"}, "/tmp", "root")
	key2 := RunOpKey([]string{"echo", "hello"}, []string{"FOO=bar"}, "/tmp", "root")
	if key1 != key2 {
		t.Errorf("RunOpKey not deterministic: %s != %s", key1, key2)
	}

	// Different command should produce different key
	key3 := RunOpKey([]string{"echo", "world"}, []string{"FOO=bar"}, "/tmp", "root")
	if key1 == key3 {
		t.Error("Different commands should produce different keys")
	}

	// Different env should produce different key
	key4 := RunOpKey([]string{"echo", "hello"}, []string{"FOO=baz"}, "/tmp", "root")
	if key1 == key4 {
		t.Error("Different env should produce different keys")
	}

	// Different workdir should produce different key
	key5 := RunOpKey([]string{"echo", "hello"}, []string{"FOO=bar"}, "/home", "root")
	if key1 == key5 {
		t.Error("Different workdir should produce different keys")
	}

	// Different user should produce different key
	key6 := RunOpKey([]string{"echo", "hello"}, []string{"FOO=bar"}, "/tmp", "nobody")
	if key1 == key6 {
		t.Error("Different user should produce different keys")
	}
}

func TestCopyOpKey(t *testing.T) {
	key1 := CopyOpKey("/src/file.txt", "/dst/file.txt", "hash123")
	key2 := CopyOpKey("/src/file.txt", "/dst/file.txt", "hash123")
	if key1 != key2 {
		t.Errorf("CopyOpKey not deterministic: %s != %s", key1, key2)
	}

	// Different src should produce different key
	key3 := CopyOpKey("/other/file.txt", "/dst/file.txt", "hash123")
	if key1 == key3 {
		t.Error("Different src should produce different keys")
	}

	// Different dst should produce different key
	key4 := CopyOpKey("/src/file.txt", "/other/file.txt", "hash123")
	if key1 == key4 {
		t.Error("Different dst should produce different keys")
	}

	// Different content hash should produce different key
	key5 := CopyOpKey("/src/file.txt", "/dst/file.txt", "hash456")
	if key1 == key5 {
		t.Error("Different content hash should produce different keys")
	}
}

func TestKeyChaining(t *testing.T) {
	// Simulate a chain of operations
	base := BaseKey("alpine:latest", "amd64")
	after1 := DeriveKey(base, RunOpKey([]string{"apk", "add", "curl"}, nil, "/", "root"))
	after2 := DeriveKey(after1, RunOpKey([]string{"apk", "add", "vim"}, nil, "/", "root"))

	// Keys should all be different
	if base == after1 {
		t.Error("Keys should differ after first op")
	}
	if after1 == after2 {
		t.Error("Keys should differ after second op")
	}
	if base == after2 {
		t.Error("Keys should differ from base after two ops")
	}

	// Same chain should be reproducible
	base2 := BaseKey("alpine:latest", "amd64")
	after1b := DeriveKey(base2, RunOpKey([]string{"apk", "add", "curl"}, nil, "/", "root"))
	after2b := DeriveKey(after1b, RunOpKey([]string{"apk", "add", "vim"}, nil, "/", "root"))

	if after2 != after2b {
		t.Errorf("Same chain should produce same final key: %s != %s", after2, after2b)
	}
}
