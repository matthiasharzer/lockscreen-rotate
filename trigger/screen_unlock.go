package trigger

import "github.com/matthiasharzer/lockscreen-rotate/lockscreen"

func ScreenUnlock(state *lockscreen.State) Trigger {
	ch := make(chan struct{})

	go func() {
		defer close(ch)

		for isLocked := range state.Subscribe() {
			if !isLocked {
				ch <- struct{}{}
			}
		}
	}()

	return ch
}
