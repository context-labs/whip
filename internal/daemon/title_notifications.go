package daemon

import "github.com/context-labs/whip/internal/protocol"

type titleListener struct {
	notify func(string)
}

// listenTitleChanges is owned by a protocol server, not by root subscriptions.
func (d *Daemon) listenTitleChanges(notify func(string)) func() {
	listener := &titleListener{notify: notify}
	d.titleMu.Lock()
	if d.titleListeners == nil {
		d.titleListeners = make(map[*titleListener]struct{})
	}
	d.titleListeners[listener] = struct{}{}
	d.titleMu.Unlock()
	return func() {
		d.titleMu.Lock()
		delete(d.titleListeners, listener)
		d.titleMu.Unlock()
	}
}

// Call only after the title commits and outside the root-registry lock.
func (d *Daemon) notifyTitleChanged(rootID string) {
	d.titleMu.Lock()
	listeners := make([]*titleListener, 0, len(d.titleListeners))
	for listener := range d.titleListeners {
		listeners = append(listeners, listener)
	}
	d.titleMu.Unlock()
	for _, listener := range listeners {
		listener.notify(rootID)
	}
}

func (s *Session) notifyTitleChanged(rootID string) {
	if s.titleChanged != nil {
		s.titleChanged(rootID)
	}
}

func (s *Server) notifyTitleChanged(rootID string) {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	connections := make([]*serverConn, 0, len(s.clients))
	for connection := range s.clients {
		// All initialized connections can read sessions.list; the transport
		// owns local/network authorization. No root subscription is required.
		if connection.titleNotifications {
			connections = append(connections, connection)
		}
	}
	s.mu.Unlock()
	for _, connection := range connections {
		connection.notify("sessions.title.changed", protocol.SessionTitleChangedParams{RootID: rootID})
	}
}
