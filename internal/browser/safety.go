// safety.go ports the URL policy floor from hermes's tools/url_safety.py:
// an unconditional always-blocked set (cloud metadata/IMDS endpoints —
// they fire even for local browsers, since a local Chrome on a cloud VM
// still reaches host IMDS), plus a private-address block for non-live
// backends, fail-closed on DNS errors, checking every DNS answer.
//
// Post-redirect recheck happens in the tool layer: after Navigate, the
// final URL is re-checked and the page neutralized to about:blank on a hit.

package browser

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// alwaysBlockedHosts are sentinel hostnames with no legitimate agent use.
var alwaysBlockedHosts = map[string]bool{
	"metadata.google.internal": true,
	"metadata":                 true,
	"instance-data":            true,
	"169.254.169.254":          true,
	"100.100.100.200":          true, // Alibaba
	"fd00:ec2::254":            true, // AWS IPv6 IMDS
}

// alwaysBlockedNets: cloud metadata IPs/nets (the floor — §2c).
var alwaysBlockedNets = []netip.Prefix{
	netip.MustParsePrefix("169.254.169.254/32"), // AWS/GCP/Azure IMDS
	netip.MustParsePrefix("169.254.170.2/32"),   // ECS task metadata
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS IPv6 IMDS
}

// CheckURL enforces the always-blocked floor on url. Every navigation —
// any mode, any backend — passes through this. DNS answers are all
// checked; resolution failure blocks (fail-closed per url_safety.py).
func CheckURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("unparseable URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil // only http(s) carries the SSRF concern
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if alwaysBlockedHosts[host] {
		return fmt.Errorf("blocked: %s is a cloud-metadata endpoint (always-blocked floor)", host)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if ipBlocked(addr) {
			return fmt.Errorf("blocked: %s is a cloud-metadata address (always-blocked floor)", host)
		}
		return nil // literal IP, not in the floor
	}
	resolver := &net.Resolver{}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("blocked: DNS resolution for %s failed (%w) — fail-closed", host, err)
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		if addr.Is4In6() {
			addr = addr.Unmap()
		}
		if ipBlocked(addr) {
			return fmt.Errorf("blocked: %s resolves to cloud-metadata address %s (always-blocked floor)", host, addr)
		}
	}
	return nil
}

func ipBlocked(addr netip.Addr) bool {
	for _, p := range alwaysBlockedNets {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// isPrivate reports whether addr is in a private/loopback/link-local/CGNAT
// range (url_safety.py _is_blocked_ip, minus the always-blocked floor).
func isPrivate(addr netip.Addr) bool {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	return cgnat.Contains(addr)
}

// CheckPrivateURL blocks private/internal targets; used for dedicated and
// headless backends where the browser's network position is whip's, but
// the page content then feeds the model — mirror of url_safety.py's
// is_safe_url for non-local backends. Live mode skips this: the user's own
// browser on their own network may legitimately browse intranet pages.
func CheckPrivateURL(ctx context.Context, rawURL string, allowPrivateURLs bool) error {
	if allowPrivateURLs {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("unparseable URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if addr, err := netip.ParseAddr(host); err == nil {
		if isPrivate(addr) {
			return fmt.Errorf("blocked: %s is a private/internal address (set browser.allowPrivateUrls to permit)", host)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := (&net.Resolver{}).LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("blocked: DNS resolution for %s failed (%w) — fail-closed", host, err)
	}
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok && isPrivate(addr) {
			return fmt.Errorf("blocked: %s resolves to private address %s (set browser.allowPrivateUrls to permit)", host, addr.Unmap())
		}
	}
	return nil
}
