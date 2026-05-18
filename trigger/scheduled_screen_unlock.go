package trigger

import (
	"errors"
	"sync"
	"time"

	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
)

func ScheduledScreenUnlock(state *lockscreen.State, minUpdateDelay, maxUpdateDelay time.Duration) (Trigger, error) {
	if minUpdateDelay > maxUpdateDelay {
		return nil, errors.New("minUpdateDelay cannot be greater than maxUpdateDelay")
	}

	ch := make(chan struct{})

	lastModificationTime := time.Time{}

	interval := NewIntervalTrigger(maxUpdateDelay)
	intervalTrigger := interval.Trigger()
	screenUnlockTrigger := ScreenUnlock(state)
	mu := sync.Mutex{}

	go func() {
		for {
			select {
			case <-intervalTrigger:
				mu.Lock()
				ch <- struct{}{}
				lastModificationTime = time.Now()
				mu.Unlock()
			case <-screenUnlockTrigger:
				mu.Lock()
				if time.Since(lastModificationTime) >= minUpdateDelay {
					ch <- struct{}{}
					lastModificationTime = time.Now()
					interval.Reset() // Sync interval to last screen unlock
				}
				mu.Unlock()
			}
		}
	}()

	return ch, nil
}
