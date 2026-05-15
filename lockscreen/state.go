package lockscreen

import (
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/matthiasharzer/lockscreen-rotate/logging"
)

type State struct {
	locked      bool
	conn        *dbus.Conn
	subscribers []chan bool

	mu sync.RWMutex
}

func NewState(conn *dbus.Conn) (*State, error) {
	s := &State{
		conn: conn,
		mu:   sync.RWMutex{},
	}

	locked, err := s.getSystemLockState()
	if err != nil {
		return nil, err
	}
	s.locked = locked

	err = s.listenForLockStateChanges()
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *State) getSystemLockState() (bool, error) {
	obj := s.conn.Object("org.freedesktop.ScreenSaver", "/ScreenSaver")
	var active bool
	err := obj.Call("org.freedesktop.ScreenSaver.GetActive", 0).Store(&active)
	if err != nil {
		return false, err
	}
	return active, nil
}

func (s *State) listenForLockStateChanges() error {
	err := s.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.ScreenSaver"),
		dbus.WithMatchMember("ActiveChanged"),
	)
	if err != nil {
		return fmt.Errorf("failed to add match signal: %w", err)
	}

	sigChan := make(chan *dbus.Signal, 10)
	s.conn.Signal(sigChan)

	// Spawn a background goroutine dedicated strictly to D-Bus events
	go func() {
		logging.Info("Listening for D-Bus ScreenSaver unlock events...")
		for sig := range sigChan {
			s.onDbusSignal(sig)
		}
	}()

	return nil
}

func (s *State) onDbusSignal(sig *dbus.Signal) {
	if sig.Name != "org.freedesktop.ScreenSaver.ActiveChanged" {
		return
	}

	if len(sig.Body) != 1 {
		logging.Warn("Received unexpected D-Bus signal with wrong number of arguments", "signal", sig)
		return
	}

	active, ok := sig.Body[0].(bool)
	if !ok {
		logging.Warn("Received unexpected D-Bus signal with wrong argument type", "signal", sig)
		return
	}

	s.mu.Lock()
	if s.locked == active {
		s.mu.Unlock()
		return
	}
	s.locked = active
	s.mu.Unlock()

	logging.Info("Screen lock state changed", "locked", active)

	s.notifySubscribers(active)
}

func (s *State) notifySubscribers(locked bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- locked:
		default:
			logging.Warn("Subscriber channel is full, skipping notification", "locked", locked)
		}
	}
}

func (s *State) Subscribe() <-chan bool {
	ch := make(chan bool, 1)

	s.mu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.mu.Unlock()

	return ch
}

func (s *State) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locked
}
