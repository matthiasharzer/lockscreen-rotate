package trigger

import (
	"sync"
	"time"
)

type IntervalTrigger struct {
	interval time.Duration
	ticker   *time.Ticker

	mu sync.RWMutex
}

func NewIntervalTrigger(interval time.Duration) *IntervalTrigger {
	return &IntervalTrigger{interval: interval,
		ticker: time.NewTicker(interval),
	}
}

func (t *IntervalTrigger) Trigger() Trigger {
	ch := make(chan struct{})
	t.ticker.Reset(t.interval)

	go func() {
		defer t.ticker.Stop()

		for {
			select {
			case _, ok := <-t.ticker.C:
				if !ok {
					return
				}
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}()

	return ch
}

func (t *IntervalTrigger) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ticker.Reset(t.interval)
}
