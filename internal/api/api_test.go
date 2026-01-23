package api

import (
	"testing"
)

func TestNewOCIClient(t *testing.T) {
	client, err := NewOCIClient()
	if err != nil {
		t.Fatalf("NewOCIClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewOCIClient() returned nil")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("generateID() returned empty string")
	}
	if id1 == id2 {
		t.Error("generateID() returned same ID twice")
	}
}
