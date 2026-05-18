package run

import (
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/matthiasharzer/lockscreen-rotate/lockscreen"
	"github.com/matthiasharzer/lockscreen-rotate/trigger"
)

func Trigger(minUpdateDelay, maxUpdateDelay time.Duration) (trigger.Trigger, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	state, err := lockscreen.NewState(conn)
	if err != nil {
		return nil, err
	}

	scheduledScreenUnlockTrigger, err := trigger.ScheduledScreenUnlock(state, minUpdateDelay, maxUpdateDelay)
	if err != nil {
		return nil, err
	}

	return scheduledScreenUnlockTrigger, nil
}
