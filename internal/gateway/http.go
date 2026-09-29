package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/context-labs/whip/internal/protocol"
)

func validateOrigins(origins []string) error {
	if len(origins) > 32 {
		return errors.New("too many gateway origins")
	}
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			return errors.New("invalid allowed browser origin")
		}
		scheme := parsed.Scheme == "http" || parsed.Scheme == "https" || origin == "whip-app://bundle"
		if !scheme || origin != parsed.Scheme+"://"+parsed.Host {
			return errors.New("browser origins must be exact HTTP origins or whip-app://bundle")
		}
	}
	return nil
}

func validateHosts(hosts []string) error {
	if len(hosts) > 32 {
		return errors.New("too many gateway hosts")
	}
	for _, host := range hosts {
		parsed, err := url.Parse("http://" + host)
		if err != nil || host == "" || parsed.Host != host || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("gateway hosts must be exact HTTP authorities")
		}
	}
	return nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/web", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(protocol.GatewayDiscovery{
			Available: s.options.Assets != nil, Major: protocol.Major,
			RuntimeID: s.options.RuntimeID, ProcessEpoch: s.options.ProcessEpoch,
			WebSocketPath: "/api/v4/ws", ContentPath: "/api/v4/content/", MaxContentBytes: maxContentBytes,
		})
	})
	if s.options.Assets != nil {
		mux.Handle("/", s.options.Assets)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "v4 browser application is not packaged", http.StatusServiceUnavailable)
		})
	}
	mux.HandleFunc("GET /api/v4/ws", s.websocket)
	mux.Handle("/api/v4/content/", s.contentHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(s.options.AllowedHosts, r.Host) {
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
		if origin != "" && origin != scheme+"://"+r.Host && !slices.Contains(s.options.AllowedOrigins, origin) {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Content-SHA256, Content-Length")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Content-SHA256")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
