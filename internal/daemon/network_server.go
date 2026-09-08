package daemon

import (
	"errors"
	"net/http"
	"time"
)

func (s *Server) startNetwork() error {
	listener, err := s.options.Network.listen(s.ctx)
	if err != nil || listener == nil {
		return err
	}
	options := s.options.Network
	if len(options.AllowedHosts) == 0 {
		options.AllowedHosts = []string{listener.Addr().String()}
	}
	handler, err := newNetworkHandler(options, s.serveTransport, func() bool {
		s.lifeMu.Lock()
		defer s.lifeMu.Unlock()
		if s.closed.Load() {
			return false
		}
		select {
		case s.slots <- struct{}{}:
			s.wg.Add(1)
			return true
		default:
			return false
		}
	}, func() { <-s.slots; s.wg.Done() }, newContentHTTPHandler(s.uploads))
	if err != nil {
		_ = listener.Close()
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	s.lifeMu.Lock()
	if s.closed.Load() {
		s.lifeMu.Unlock()
		return listener.Close()
	}
	s.httpServer = server
	s.networkListener = listener
	s.networkEndpoint = "http://" + listener.Addr().String()
	s.wg.Go(func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.cancel()
			s.lifeMu.Lock()
			if s.listener != nil {
				_ = s.listener.Close()
			}
			s.lifeMu.Unlock()
		}
	})
	s.lifeMu.Unlock()
	return nil
}

func writeTransportMessage(transport messageTransport, message rpcMessage) error {
	frame, err := marshalFrame(message)
	if err != nil {
		return err
	}
	return transport.WriteMessage(frame)
}
