package protocol

// NetworkClientCapability requests immutable network-client restrictions on a
// connection, including when its transport is a local socket. A gateway must
// require this capability in NegotiatedCapabilities before forwarding traffic.
const NetworkClientCapability = "network-client-v1"

// GatewayStatus describes only the gateway child managed by this daemon.
// State is disabled, starting, ready, or failed. Foreground gateways are not
// advertised here; an endpoint is usable only while the state is ready.
type GatewayStatus struct {
	State    string `json:"state"`
	Endpoint string `json:"endpoint,omitempty"`
	Error    string `json:"error,omitempty"`
}
