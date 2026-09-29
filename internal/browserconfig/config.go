// Package browserconfig declares external Chrome availability. A declaration
// neither launches Chrome nor grants control of a browser or its files.
package browserconfig

import (
	"errors"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Config struct {
	Mode             string `json:"mode"`
	Executable       string `json:"executable"`
	LiveEndpoint     string `json:"live_endpoint"`
	LiveProfile      string `json:"live_profile"`
	AllowPrivateURLs bool   `json:"allow_private_urls"`
}

func (c Config) Normalize() (Config, error) {
	if c.Mode == "" {
		c.Mode = "disabled"
	}
	switch c.Mode {
	case "disabled", "live", "dedicated", "headless", "extension":
	default:
		return Config{}, errors.New("invalid external browser mode")
	}
	for _, path := range []string{c.Executable, c.LiveProfile} {
		if path != "" && (len(path) > 4096 || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) || filepath.Clean(path) != path) {
			return Config{}, errors.New("external browser path must be clean and absolute")
		}
	}
	if c.LiveEndpoint != "" {
		if err := ValidateEndpoint(c.LiveEndpoint); err != nil {
			return Config{}, err
		}
	}
	if c.LiveEndpoint != "" && c.LiveProfile != "" {
		return Config{}, errors.New("select one live endpoint or profile")
	}
	if c.Mode == "live" && c.LiveEndpoint == "" && c.LiveProfile == "" {
		return Config{}, errors.New("live browser requires an explicit endpoint or profile")
	}
	if (c.Mode == "dedicated" || c.Mode == "headless") && c.Executable == "" {
		return Config{}, errors.New("launched browser requires an explicit executable")
	}
	return c, nil
}

// ValidateEndpoint permits only literal loopback CDP endpoints without bearer
// credentials. Relay credentials are process-owned and never host configuration.
func ValidateEndpoint(raw string) error {
	if len(raw) > 4096 || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\x00\r\n") {
		return errors.New("invalid live browser endpoint")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Scheme != "ws" && u.Scheme != "http") {
		return errors.New("live browser requires a loopback HTTP or WebSocket endpoint without credentials")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" {
		return errors.New("live browser endpoint must use a literal loopback address")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("live browser endpoint requires a port")
	}
	if u.Scheme == "http" && u.Path != "" && u.Path != "/" {
		return errors.New("HTTP browser endpoint must have no path")
	}
	if u.Scheme == "ws" && (!strings.HasPrefix(u.Path, "/devtools/browser/") || len(strings.TrimPrefix(u.Path, "/devtools/browser/")) == 0) {
		return errors.New("WebSocket browser endpoint must identify a browser")
	}
	return nil
}
