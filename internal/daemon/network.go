package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/webassets"
	"github.com/gobwas/ws"
)

// NetworkOptions enables access from a local browser or explicitly trusted network.
// Client identifiers on this listener are command namespaces, not authenticated identities.
type NetworkOptions struct {
	Enabled        bool     `json:"enabled"`
	Address        string   `json:"address,omitempty"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
	AllowedHosts   []string `json:"allowed_hosts,omitempty"`
	// Terminals lets network clients open workspace terminals. Unix-socket
	// clients, including SSH-forwarded ones, are always allowed.
	Terminals bool `json:"terminals,omitempty"`
}

func (o NetworkOptions) listen(ctx context.Context) (net.Listener, error) {
	if !o.Enabled {
		return nil, nil //nolint:nilnil // a disabled optional network listener is not an error
	}
	address := o.Address
	if address == "" {
		address = "127.0.0.1:0"
	}
	return (&net.ListenConfig{}).Listen(ctx, "tcp", address)
}

// newNetworkHandler receives an already reserved connection slot. Returning false
// from reserve rejects the upgrade before hijacking; release runs after accept.
func newNetworkHandler(
	options NetworkOptions,
	accept func(messageTransport),
	reserve func() bool,
	release func(),
	content http.Handler,
) (http.Handler, error) {
	if len(options.AllowedHosts) == 0 {
		return nil, errors.New("network listener requires exact accepted hosts")
	}
	for _, origin := range options.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			return nil, errors.New("invalid allowed browser origin")
		}
		validScheme := parsed.Scheme == "http" || parsed.Scheme == "https" || origin == "whip-app://bundle"
		if !validScheme || origin != parsed.Scheme+"://"+parsed.Host {
			return nil, errors.New("browser origins must be exact HTTP origins or whip-app://bundle")
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/web", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Available     bool   `json:"available"`
			ProtocolMajor int    `json:"protocol_major"`
			WebSocketPath string `json:"websocket_path"`
			ContentPath   string `json:"content_path"`
		}{Available: webassets.Available(), ProtocolMajor: ProtocolMajor, WebSocketPath: "/api/v3/ws", ContentPath: "/api/v3/content/"})
	})
	mux.Handle("/", webassets.Handler())
	mux.HandleFunc("GET /api/v3/ws", func(w http.ResponseWriter, r *http.Request) {
		if !reserve() {
			http.Error(w, "connection limit reached", http.StatusServiceUnavailable)
			return
		}
		defer release()
		conn, buffered, _, err := (ws.HTTPUpgrader{Timeout: 5 * time.Second}).Upgrade(r, w)
		// gobwas hijacks before validating the request. Rejections return a socket
		// too; net/http no longer owns it, so both paths must close it.
		if conn != nil {
			defer func() { _ = conn.Close() }()
		}
		if err != nil {
			return
		}
		accept(newWebsocketMessageTransport(conn, buffered.Reader))
	})
	if content != nil {
		mux.Handle("/api/v3/content/", content)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !options.Enabled {
			http.NotFound(w, r)
			return
		}
		if !slices.Contains(options.AllowedHosts, r.Host) {
			http.Error(w, "host is not allowed", http.StatusForbidden)
			return
		}
		origins := r.Header.Values("Origin")
		if len(origins) > 1 {
			http.Error(w, "ambiguous origin", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		sameOrigin := scheme + "://" + r.Host
		if origin != "" && origin != sameOrigin && !slices.Contains(options.AllowedOrigins, origin) {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Content-SHA256")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}
