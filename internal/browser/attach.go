// attach.go ports browser-harness daemon.py's live-browser discovery:
// scan well-known Chromium profile dirs for DevToolsActivePort, verify the
// browser process actually holds the profile (SingletonLock), and resolve
// the file's port+path (or an explicit endpoint) to a WebSocket debugger
// URL — including Chrome 147+'s disabled /json/* on the default profile
// and Chrome 144+'s per-connection permission popup as structured errors.

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ProfileDir is a well-known Chromium-family user-data dir (daemon.py's
// _MAC_PROFILES/_LINUX_PROFILES/_WINDOWS_PROFILES).
func profileDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var rel []string
	switch runtime.GOOS {
	case "darwin":
		rel = []string{
			"Library/Application Support/Google/Chrome",
			"Library/Application Support/Google/Chrome Canary",
			"Library/Application Support/Comet",
			"Library/Application Support/Arc/User Data",
			"Library/Application Support/Dia/User Data",
			"Library/Application Support/Microsoft Edge",
			"Library/Application Support/Microsoft Edge Beta",
			"Library/Application Support/Microsoft Edge Dev",
			"Library/Application Support/Microsoft Edge Canary",
			"Library/Application Support/BraveSoftware/Brave-Browser",
		}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		rel = []string{
			"Google/Chrome/User Data",
			"Google/Chrome SxS/User Data",
			"Google/Chrome Beta/User Data",
			"Google/Chrome Dev/User Data",
			"Chromium/User Data",
			"Microsoft/Edge/User Data",
			"Microsoft/Edge Beta/User Data",
			"Microsoft/Edge Dev/User Data",
			"Microsoft/Edge SxS/User Data",
			"BraveSoftware/Brave-Browser/User Data",
		}
		var out []string
		for _, r := range rel {
			out = append(out, filepath.Join(local, filepath.FromSlash(r)))
		}
		return out
	default: // linux & friends
		rel = []string{
			".config/google-chrome",
			".config/chromium",
			".config/chromium-browser",
			".config/microsoft-edge",
			".config/microsoft-edge-beta",
			".config/microsoft-edge-dev",
			".var/app/org.chromium.Chromium/config/chromium",
			".var/app/com.google.Chrome/config/google-chrome",
			".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser",
			".var/app/com.microsoft.Edge/config/microsoft-edge",
		}
	}
	var out []string
	for _, r := range rel {
		out = append(out, filepath.Join(home, filepath.FromSlash(r)))
	}
	return out
}

// parseDevToolsActivePort reads the two-line file Chrome writes:
// line 1 = port, line 2 = WS path (daemon.py get_ws_url).
func parseDevToolsActivePort(data []byte) (port int, wsPath string, err error) {
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("DevToolsActivePort: want 2 lines, got %d", len(lines))
	}
	port, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", fmt.Errorf("DevToolsActivePort port: %w", err)
	}
	wsPath = strings.TrimSpace(lines[1])
	if wsPath == "" {
		return 0, "", errors.New("DevToolsActivePort: empty ws path")
	}
	return port, wsPath, nil
}

// browserRunningForProfile reports whether a live process holds this
// user-data-dir, via the SingletonLock symlink's embedded PID (POSIX;
// daemon.py browser_running_for_profile). Unknown/Windows answers false —
// discovery then relies on the port being reachable.
func browserRunningForProfile(base string) bool {
	if runtime.GOOS == "windows" {
		return true // Chromium on Windows uses a named mutex; assume running.
	}
	target, err := os.Readlink(filepath.Join(base, "SingletonLock"))
	if err != nil {
		return false
	}
	pidStr := target[strings.LastIndex(target, "-")+1:]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 = existence probe (os.kill(pid, 0) in the Python).
	return proc.Signal(os.Signal(sigzero())) == nil
}

// portLive is daemon.py's _devtools_port_live: a stale DevToolsActivePort
// file left by a closed browser must not count. Both loopbacks are probed
// (hermes browser_connect.py's _LOOPBACK_PROBE_HOSTS): a squatter on
// 127.0.0.1:<port> pushes Chrome to bind [::1] only.
func portLive(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// chromeWSURL reads base's /json/version and returns the browser-level WS
// debugger URL, or "" when the endpoint isn't a debuggable Chromium (e.g. a
// dev server squatting the port answers 404 HTML — a false positive here
// would hand rod a bogus WebSocket URL).
func chromeWSURL(ctx context.Context, base string) (string, error) {
	//nolint:gosec // G704: base is a local browser debug endpoint discovered from the profile dir, not user input
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/version", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req) //nolint:gosec // G704: local browser debug endpoint (see above)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("%w: Chrome is reachable, but the per-session 'Allow remote debugging' popup has not been accepted — click Allow in Chrome, then retry", ErrPermissionBlocked)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	var v struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return "", nil
	}
	// The Browser field identifies a Chromium-family endpoint: Chrome,
	// Chromium, HeadlessChrome — and the Chromium forks profileDirs scans
	// (Edge answers "Edge/…", Brave "Brave/…" on some versions). Absent or
	// unrecognised → not a browser we can drive (a squatter's JSON), so "".
	family := []string{"Chrom", "Edge", "Brave"}
	recognised := false
	for _, f := range family {
		if strings.Contains(v.Browser, f) {
			recognised = true
			break
		}
	}
	if !recognised {
		return "", nil
	}
	return v.WebSocketDebuggerURL, nil
}

// resolveWSURL turns a DevTools endpoint into the WebSocket debugger URL,
// per daemon.py get_ws_url with hermes browser_connect.py's dual-stack
// hardening. scheme is "http" or "https" (explicit endpoints may be TLS);
// empty defaults to http. host selects the probe family: "" probes v4 then
// v6 loopbacks (a non-Chrome squatter on 127.0.0.1:<port> leaves Chrome
// listening on [::1] only), any other value probes just that host.
// /json/version normally answers; on a squatter's junk response (Chrome
// 147+ also disables /json/* on the default profile) fall back to the
// DevToolsActivePort file's WS path — trustworthy only in the profile-scan
// path, where the file + live lock + live port come from the same profile.
func resolveWSURL(ctx context.Context, scheme, host string, port int, wsPath string) (string, error) {
	if scheme == "" {
		scheme = "http"
	}
	wsScheme := "ws"
	if scheme == "https" {
		wsScheme = "wss"
	}
	var hosts []string
	if host == "::1" {
		hosts = []string{"::1"}
	} else if host != "" {
		hosts = []string{host}
	} else {
		hosts = []string{"127.0.0.1", "::1"}
	}
	var permErr error
	for _, h := range hosts {
		base := scheme + "://" + net.JoinHostPort(h, strconv.Itoa(port))
		ws, err := chromeWSURL(ctx, base)
		if err != nil {
			if errors.Is(err, ErrPermissionBlocked) {
				permErr = err
			}
			continue
		}
		if ws != "" {
			return ws, nil
		}
	}
	if permErr != nil {
		return "", permErr
	}
	// Chrome 147+ disables /json/* on the default user-data-dir: fall back
	// to the DevToolsActivePort file's WS path — but only after the path
	// proves it's a real DevTools endpoint by answering a WebSocket upgrade
	// (101). A non-Chrome squatter on the port (node dev server, IDE
	// debugger) holds the file's port hostage and would otherwise be handed
	// to rod as a bogus ws:// URL — the node-on-9222 failure class.
	if wsPath != "" {
		for _, h := range hosts {
			if wsUpgradeAnswers(ctx, scheme, h, port, wsPath) {
				return wsScheme + "://" + net.JoinHostPort(h, strconv.Itoa(port)) + wsPath, nil
			}
		}
	}
	return "", fmt.Errorf("%s: no debuggable Chromium endpoint", net.JoinHostPort(hosts[0], strconv.Itoa(port)))
}

// wsUpgradeAnswers reports whether http://host:port+path completes the
// WebSocket handshake (101 Switching Protocols) — the minimum proof that a
// DevToolsActivePort-file path still names a live DevTools endpoint and not
// a squatter's HTTP server.
func wsUpgradeAnswers(ctx context.Context, scheme, host string, port int, path string) bool {
	//nolint:gosec // G704: probing the local DevTools endpoint named by the browser's own DevToolsActivePort file
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		scheme+"://"+net.JoinHostPort(host, strconv.Itoa(port))+path, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==") // fixed probe key; we never read the socket
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req) //nolint:gosec // G704: local DevTools probe (see above)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusSwitchingProtocols
}

// DiscoverLiveWS finds a running Chromium-family browser with remote
// debugging enabled and returns its browser-level WebSocket URL. Explicit
// endpoints win: WHIP_CDP_WS (ws:// URL, used verbatim) then WHIP_CDP_URL
// (http:// endpoint, resolved). Otherwise the profile scan runs.
func DiscoverLiveWS(ctx context.Context) (string, error) {
	if ws := os.Getenv("WHIP_CDP_WS"); ws != "" {
		return ws, nil
	}
	if httpURL := os.Getenv("WHIP_CDP_URL"); httpURL != "" {
		scheme, host, port := splitEndpoint(httpURL)
		return resolveWSURL(ctx, scheme, host, port, "")
	}
	var sawStaleFile bool
	for _, base := range profileDirs() {
		ws, stale, err := discoverProfileWS(ctx, base)
		if err == nil {
			return ws, nil
		}
		if stale {
			sawStaleFile = true
		}
		if errors.Is(err, ErrPermissionBlocked) {
			return "", err // permission beats continuing the scan
		}
	}
	// Fallback probe of the conventional ports (daemon.py's last resort).
	for _, port := range []int{9222, 9223} {
		if !portLive(port) {
			continue
		}
		ws, err := resolveWSURL(ctx, "", "", port, "")
		if err == nil {
			return ws, nil
		}
		if errors.Is(err, ErrPermissionBlocked) {
			return "", err
		}
	}
	if sawStaleFile {
		return "", fmt.Errorf("%w: a closed browser left a stale DevToolsActivePort file — reopen Chrome with remote debugging enabled (chrome://inspect/#remote-debugging), or run in dedicated/headless mode", ErrNoLiveBrowser)
	}
	return "", fmt.Errorf("%w: no supported Chromium-family browser with remote debugging is running — enable chrome://inspect/#remote-debugging in Chrome, start Chrome with --remote-debugging-port=9222, or use dedicated/headless mode", ErrNoLiveBrowser)
}

// DiscoverWSForProfile resolves a live browser's WS URL from one specific
// user-data-dir's DevToolsActivePort — used to reattach to a previously
// launched whipcode Chrome instead of spawning a duplicate (hermes
// /browser connect's already-listening check, via the profile file).
// ok is false when the file is absent or the browser behind it is gone.
func DiscoverWSForProfile(ctx context.Context, base string) (ws string, ok bool) {
	ws, stale, err := discoverProfileWS(ctx, base)
	return ws, err == nil && !stale
}

// discoverProfileWS is the shared scan body: read <base>/DevToolsActivePort,
// verify the port answers and a live process holds the profile lock, then
// resolve the WS URL. stale reports a port file whose browser is gone.
func discoverProfileWS(ctx context.Context, base string) (ws string, stale bool, err error) {
	data, err := os.ReadFile(filepath.Join(base, "DevToolsActivePort")) //nolint:gosec // G304: base is the browser profile dir whipcode itself resolved
	if err != nil {
		return "", false, err
	}
	port, wsPath, err := parseDevToolsActivePort(data)
	if err != nil {
		return "", false, err
	}
	if !portLive(port) {
		return "", true, fmt.Errorf("stale DevToolsActivePort in %s", base)
	}
	if !browserRunningForProfile(base) {
		return "", false, fmt.Errorf("no live process holds %s", base)
	}
	ws, err = resolveWSURL(ctx, "", "", port, wsPath)
	return ws, false, err
}

func splitHostPort(httpURL string) (string, int) {
	u := strings.TrimPrefix(strings.TrimPrefix(httpURL, "http://"), "https://")
	host, portStr, err := net.SplitHostPort(strings.TrimSuffix(u, "/"))
	if err != nil {
		return u, 80
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

// splitEndpoint splits an explicit CDP endpoint into scheme/host/port,
// preserving https (a remote Chrome behind TLS) that splitHostPort strips.
func splitEndpoint(rawurl string) (scheme, host string, port int) {
	scheme = "http"
	if strings.HasPrefix(rawurl, "https://") {
		scheme = "https"
	}
	if strings.HasPrefix(rawurl, "wss://") {
		scheme = "https" // wss endpoints resolve like https
	}
	host, port = splitHostPort(rawurl)
	return scheme, host, port
}
