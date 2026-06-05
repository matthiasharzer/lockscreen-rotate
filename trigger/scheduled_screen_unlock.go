package trigger

import (
	"errors"
	"time"

	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
)

func ScheduledScreenUnlock(state *lockscreen.State, minUpdateDelay, maxUpdateDelay time.Duration) (Trigger, error) {
	if minUpdateDelay > maxUpdateDelay {
		return nil, errors.New("minUpdateDelay cannot be greater than maxUpdateDelay")
	}

	ch := make(chan struct{})

	interval := NewIntervalTrigger(maxUpdateDelay)
	screenUnlockTrigger := ScreenUnlock(state)

	go func() {
		ch <- struct{}{} // Trigger immediately on startup
		intervalTrigger := interval.Trigger()
		lastModificationTime := time.Now()

		for {
			select {
			case <-intervalTrigger:
				ch <- struct{}{}
				lastModificationTime = time.Now()
			case <-screenUnlockTrigger:
				if time.Since(lastModificationTime) >= minUpdateDelay {
					ch <- struct{}{}
					lastModificationTime = time.Now()
					interval.Reset() // Sync interval to last screen unlock
				}
			}
		}
	}()

	return ch, nil
}
