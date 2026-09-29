package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const MaxRemoteHosts = 16

// RemoteHost is a saved browser target. Its identity is a caller-observed pin,
// never authority to connect, execute work, or resolve credentials.
type RemoteHost struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	RuntimeID       string `json:"runtime_id"`
	ConnectOnLaunch bool   `json:"connect_on_launch"`
}

// NormalizeRemoteHosts copies a bounded declaration, trimming display names
// while retaining each exact root URL. No network or filesystem reads occur.
func NormalizeRemoteHosts(profiles []RemoteHost) ([]RemoteHost, error) {
	if len(profiles) > MaxRemoteHosts {
		return nil, fmt.Errorf("%w: too many saved hosts", session.ErrInvalid)
	}
	result := make([]RemoteHost, 0, len(profiles))
	ids, endpoints, runtimes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, profile := range profiles {
		if profile.ID == "local" {
			return nil, fmt.Errorf("%w: a remote profile cannot replace local", session.ErrInvalid)
		}
		if session.ValidateID(profile.ID) != nil || session.ValidateID(profile.RuntimeID) != nil {
			return nil, fmt.Errorf("%w: saved host requires valid profile and runtime identities", session.ErrInvalid)
		}
		profile.Name = strings.TrimSpace(profile.Name)
		badName := profile.Name == "" || !utf8.ValidString(profile.Name) || utf8.RuneCountInString(profile.Name) > 256
		if badName || strings.IndexFunc(profile.Name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return nil, fmt.Errorf("%w: invalid saved host name", session.ErrInvalid)
		}
		endpoint, err := remoteEndpoint(profile.URL)
		if err != nil {
			return nil, err
		}
		if ids[profile.ID] || endpoints[endpoint] || runtimes[profile.RuntimeID] {
			return nil, fmt.Errorf("%w: duplicate saved host identity or endpoint", session.ErrInvalid)
		}
		ids[profile.ID], endpoints[endpoint], runtimes[profile.RuntimeID] = true, true, true
		result = append(result, profile)
	}
	return result, nil
}

func remoteEndpoint(value string) (string, error) {
	invalid := fmt.Errorf("%w: saved host requires a root HTTP(S) URL without credentials, query, or fragment", session.ErrInvalid)
	badBytes := len(value) == 0 || len(value) > 2048 || strings.IndexFunc(value, func(r rune) bool { return r < 33 || r > 126 }) >= 0
	if badBytes || strings.ContainsAny(value, "?#") {
		return "", invalid
	}
	endpoint, err := url.Parse(value)
	if err != nil {
		return "", invalid // Parse errors may include URL credentials; never project them.
	}
	badScheme := !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://")
	badScope := endpoint.User != nil || endpoint.Opaque != "" || endpoint.RawPath != ""
	badPath := endpoint.Path != "" && endpoint.Path != "/"
	if badScheme || badScope || badPath || endpoint.Hostname() == "" {
		return "", invalid
	}
	hostname := strings.ToLower(endpoint.Hostname())
	if net.ParseIP(hostname) == nil {
		badName := strings.HasPrefix(endpoint.Host, "[") || len(hostname) > 253
		badCharacter := strings.IndexFunc(hostname, func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' && r != '_'
		}) >= 0
		if badName || badCharacter || strings.Trim(hostname, ".") == "" {
			return "", invalid
		}
	} else {
		if strings.Contains(hostname, ":") && !strings.HasPrefix(endpoint.Host, "[") {
			return "", invalid
		}
		hostname = net.ParseIP(hostname).String()
	}
	port := endpoint.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", invalid
		}
		port = strconv.Itoa(number)
	} else if strings.HasSuffix(endpoint.Host, ":") {
		return "", invalid
	}
	if endpoint.Scheme == "http" && port == "80" || endpoint.Scheme == "https" && port == "443" {
		port = ""
	}
	// This comparison key detects aliases without silently rewriting the saved URL.
	return endpoint.Scheme + "://" + net.JoinHostPort(strings.TrimSuffix(hostname, "."), port), nil
}

func (a *Authority) SetRemoteHosts(ctx context.Context, expected string, profiles []RemoteHost) (Snapshot, error) {
	values, err := NormalizeRemoteHosts(profiles)
	if err != nil {
		return Snapshot{}, err
	}
	return a.Update(ctx, expected, func(host *Host) error {
		if len(values) == 0 && len(host.RemoteHosts) == 0 {
			return nil
		}
		host.RemoteHosts = values
		return nil
	})
}
