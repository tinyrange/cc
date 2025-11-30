package chipset

import (
	"sync"
	"time"
)

// timerHandle tracks a cancellable periodic callback.
type timerHandle interface {
	Stop()
}

type timerHandleFunc func()

func (f timerHandleFunc) Stop() {
	if f != nil {
		f()
	}
}

type timerFactory func(period time.Duration, cb func()) timerHandle

func defaultTimerFactory(period time.Duration, cb func()) timerHandle {
	if period <= 0 || cb == nil {
		return nil
	}

	stop := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cb()
			case <-stop:
				return
			}
		}
	}()

	return timerHandleFunc(func() {
		once.Do(func() { close(stop) })
	})
}
