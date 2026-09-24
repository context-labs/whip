package webgateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/webassets"
)

func validateOrigins(origins []string) error {
	for _, origin := range origins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			return errors.New("invalid allowed browser origin")
		}
		validScheme := parsed.Scheme == "http" || parsed.Scheme == "https" || origin == "whip-app://bundle"
		if !validScheme || origin != parsed.Scheme+"://"+parsed.Host {
			return errors.New("browser origins must be exact HTTP origins or whip-app://bundle")
		}
	}
	return nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/web", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Available     bool   `json:"available"`
			ProtocolMajor int    `json:"protocol_major"`
			WebSocketPath string `json:"websocket_path"`
			ContentPath   string `json:"content_path"`
		}{webassets.Available(), protocol.Major, "/api/v3/ws", "/api/v3/content/"})
	})
	mux.Handle("/", webassets.Handler())
	mux.HandleFunc("GET /api/v3/ws", s.websocket)
	mux.Handle("/api/v3/content/", s.contentHandler())
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
		sameOrigin := scheme + "://" + r.Host
		if origin != "" && origin != sameOrigin && !slices.Contains(s.options.AllowedOrigins, origin) {
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
	})
}
