package daemon

import "github.com/context-labs/whip/internal/protocol"

// SetGatewayStatus publishes managed-child discovery without owning a listener.
// The process owner must clear readiness before stopping or replacing the child.
func (s *Server) SetGatewayStatus(status protocol.GatewayStatus) {
	if status.State != "ready" {
		status.Endpoint = ""
	}
	s.mu.Lock()
	s.gatewayStatus = status
	s.mu.Unlock()
}

// GatewayStatus returns the current managed-child discovery state.
func (s *Server) GatewayStatus() protocol.GatewayStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gatewayStatus
}

func (s *Server) gatewayEndpoint() string {
	status := s.GatewayStatus()
	if status.State != "ready" {
		return ""
	}
	return status.Endpoint
}
