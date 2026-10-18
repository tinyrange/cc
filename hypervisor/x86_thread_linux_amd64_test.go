//go:build linux && amd64

package hypervisor

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestX86ThreadLifetimeOwnership(t *testing.T) {
	owners := []*x86Thread{newX86Thread(), newX86Thread()}
	defer func() {
		for _, owner := range owners {
			owner.close()
		}
	}()
	ids := []int32{owners[0].tid.Load(), owners[1].tid.Load()}
	if ids[0] == 0 || ids[0] == ids[1] {
		t.Fatalf("owners share thread: %v", ids)
	}
	var wg sync.WaitGroup
	for caller := 0; caller < 8; caller++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Deliberately keep callers on different threads from both owners.
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for round := 0; round < 20; round++ {
				for i, owner := range owners {
					err := owner.call(func() error {
						if int32(unix.Gettid()) != ids[i] {
							return fmt.Errorf("owner migrated")
						}
						runtime.Gosched()
						// Callbacks must be able to inspect their own CPU without deadlock.
						return owner.call(func() error {
							if int32(unix.Gettid()) != ids[i] {
								return fmt.Errorf("nested operation migrated")
							}
							return nil
						})
					})
					if err != nil {
						t.Error(err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	for _, owner := range owners {
		owner.close()
		if owner.tid.Load() != 0 {
			t.Error("close did not join owner")
		}
		if err := owner.call(func() error { t.Error("executed after close"); return nil }); err == nil {
			t.Error("accepted operation after close")
		}
	}
}
