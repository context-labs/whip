package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// RemoteHost is a browser attachment target, not an execution or credential store.
type RemoteHost struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	RuntimeID       string `json:"runtime_id"`
	ConnectOnLaunch bool   `json:"connect_on_launch"`
}

// NormalizeRemoteHosts validates a profile update without changing the caller's slice.
func NormalizeRemoteHosts(hosts []RemoteHost) ([]RemoteHost, error) {
	normalized := make([]RemoteHost, 0, len(hosts))
	ids := make(map[string]bool, len(hosts))
	endpoints := make(map[string]bool, len(hosts))
	runtimes := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		if host.ID == "local" {
			return nil, errors.New("a remote host cannot replace Local")
		}
		host.Name = strings.TrimSpace(host.Name)
		for _, identity := range []string{host.ID, host.Name, host.RuntimeID} {
			if strings.TrimSpace(identity) == "" || strings.ContainsAny(identity, "\r\n\t\x00") {
				return nil, errors.New("remote host requires an ID, name, and verified runtime ID")
			}
		}
		endpoint, err := url.Parse(strings.TrimSpace(host.URL))
		if err != nil || endpoint.Hostname() == "" {
			return nil, fmt.Errorf("remote host %q requires a daemon URL", host.Name)
		}
		switch endpoint.Scheme {
		case "http", "https":
		case "ws":
			endpoint.Scheme = "http"
		case "wss":
			endpoint.Scheme = "https"
		default:
			return nil, fmt.Errorf("remote host %q requires an HTTP or WebSocket URL", host.Name)
		}
		hasURLSecrets := endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != ""
		if hasURLSecrets {
			return nil, fmt.Errorf("remote host %q URL cannot contain credentials, a query, or a fragment", host.Name)
		}
		switch endpoint.Path {
		case "", "/", "/api/v3/ws":
		default:
			return nil, fmt.Errorf("remote host %q requires a root-mounted daemon endpoint", host.Name)
		}
		endpoint.Path, endpoint.RawPath = "", ""
		endpoint.Host = strings.ToLower(endpoint.Host)
		host.URL = endpoint.String()
		if ids[host.ID] || endpoints[host.URL] || runtimes[host.RuntimeID] {
			return nil, fmt.Errorf("remote host %q duplicates a saved profile, endpoint, or runtime", host.Name)
		}
		ids[host.ID], endpoints[host.URL], runtimes[host.RuntimeID] = true, true, true
		normalized = append(normalized, host)
	}
	return normalized, nil
}
