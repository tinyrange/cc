package main

import (
	"sync"
	"sync/atomic"
)

const numShards = 64

// handleShard is one shard of the handle table.
type handleShard struct {
	mu      sync.RWMutex
	handles map[uint64]any
}

var (
	shards     [numShards]handleShard
	nextHandle atomic.Uint64
)

func init() {
	for i := range shards {
		shards[i].handles = make(map[uint64]any)
	}
	// Start handles at 1 so 0 can be "invalid"
	nextHandle.Store(1)
}

// getShard returns the shard for a given handle.
func getShard(h uint64) *handleShard {
	return &shards[h%numShards]
}

// newHandle allocates a new handle for the given value.
func newHandle(v any) uint64 {
	h := nextHandle.Add(1) - 1
	shard := getShard(h)
	shard.mu.Lock()
	shard.handles[h] = v
	shard.mu.Unlock()
	return h
}

// getHandle retrieves the value for a handle, or nil if not found.
func getHandle(h uint64) any {
	if h == 0 {
		return nil
	}
	shard := getShard(h)
	shard.mu.RLock()
	v := shard.handles[h]
	shard.mu.RUnlock()
	return v
}

// getHandleTyped retrieves a handle and type-asserts it to T.
// Returns (value, true) on success, (zero, false) if not found or wrong type.
func getHandleTyped[T any](h uint64) (T, bool) {
	v := getHandle(h)
	if v == nil {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	return typed, ok
}

// freeHandle removes a handle from the table.
// Returns the value that was stored, or nil if not found.
func freeHandle(h uint64) any {
	if h == 0 {
		return nil
	}
	shard := getShard(h)
	shard.mu.Lock()
	v := shard.handles[h]
	delete(shard.handles, h)
	shard.mu.Unlock()
	return v
}

// freeHandleTyped removes a handle and returns the typed value.
// Returns (value, true) on success, (zero, false) if not found or wrong type.
func freeHandleTyped[T any](h uint64) (T, bool) {
	v := freeHandle(h)
	if v == nil {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	return typed, ok
}
