package trigger

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIntervalTrigger(t *testing.T) {
	t.Run("triggers at intervals", func(t *testing.T) {
		trigger := NewIntervalTrigger(100 * time.Millisecond)

		select {
		case <-trigger.Trigger():
			// Triggered as expected
		case <-time.After(150 * time.Millisecond):
			t.Fatal("expected trigger to fire within 150ms")
		}
	})

	t.Run("trigger resets correctly", func(t *testing.T) {
		trigger := NewIntervalTrigger(100 * time.Millisecond)

		<-time.After(50 * time.Millisecond)

		start := time.Now()

		trigger.Reset()

		select {
		case <-trigger.Trigger():
			now := time.Now()
			assert.WithinDuration(t, start.Add(100*time.Millisecond), now, 10*time.Millisecond, "trigger should fire approximately 100ms after reset")
			// Triggered as expected after reset
		case <-time.After(150 * time.Millisecond):
			t.Fatal("expected trigger to fire within 150ms after reset")
		}
	})
}
