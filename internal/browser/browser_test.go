package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

// noEnvCDP skips the test when explicit CDP env endpoints bypass discovery.
func noEnvCDP(t *testing.T) {
	t.Helper()
	if os.Getenv("WHIP_CDP_WS") != "" || os.Getenv("WHIP_CDP_URL") != "" {
		t.Skip("explicit CDP endpoint set — profile scan bypassed")
	}
}

// parseDevToolsActivePort: the two-line file Chrome writes.
func TestParseDevToolsActivePort(t *testing.T) {
	port, path, err := parseDevToolsActivePort([]byte("9222\n/devtools/browser/abc-123\n"))
	if err != nil || port != 9222 || path != "/devtools/browser/abc-123" {
		t.Fatalf("got %d %q %v", port, path, err)
	}
	if _, _, err := parseDevToolsActivePort([]byte("9222\n")); err == nil {
		t.Fatal("one line must fail")
	}
	if _, _, err := parseDevToolsActivePort([]byte("notaport\n/x")); err == nil {
		t.Fatal("non-numeric port must fail")
	}
}

// Profile scan finds a DevToolsActivePort in a fake profile dir.
func TestProfileScanFindsPortFile(t *testing.T) {
	noEnvCDP(t)
	if os.Getenv("WHIP_CDP_WS") != "" || os.Getenv("WHIP_CDP_URL") != "" {
		t.Skip("explicit CDP endpoint set — profile scan bypassed")
	}
	for _, p := range []int{9222, 9223} { // a real Chrome here wins the fallback probe
		if portLive(p) {
			t.Skipf("a real browser is listening on %d", p)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	profiles := profileDirs()
	if len(profiles) == 0 {
		t.Skip("no browser profile locations on this platform")
	}
	prof := profiles[0]
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	// Closed browser: file exists but nothing listens → ErrNoLiveBrowser
	// with the stale-file hint, not a hang.
	if err := os.WriteFile(filepath.Join(prof, "DevToolsActivePort"), []byte("1\n/devtools/browser/dead\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := DiscoverLiveWS(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stale DevToolsActivePort") {
		t.Fatalf("want stale-file error, got %v", err)
	}
}

// Live discovery resolves a running "browser" (httptest server) via
// /json/version → webSocketDebuggerUrl.
func TestDiscoverViaJSONVersion(t *testing.T) {
	noEnvCDP(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			fmt.Fprintf(w, `{"Browser":"HeadlessChrome/150.0","webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/browser/xyz"}`, port)
			return
		}
		http.NotFound(w, r)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	prof := filepath.Join(home, ".config", "chromium")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	// SingletonLock must point at a live PID (ours) or discovery skips it.
	if err := os.Symlink(fmt.Sprintf("testhost-%d", os.Getpid()), filepath.Join(prof, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prof, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port)+"\n/devtools/browser/xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hermetic per-profile discovery (DiscoverLiveWS's full scan is
	// platform-dependent and picks up ambient debug ports).
	ws, _, err := discoverProfileWS(context.Background(), prof)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/xyz", port); ws != want {
		t.Fatalf("got %q want %q", ws, want)
	}
}

// Chrome 147+ behavior: /json/version 404 → fall back to the file's ws
// path — but only after that path proves it's a live DevTools endpoint by
// answering a WebSocket upgrade. A plain 404 server (or squatter) must be
// refused; a handshake-answering endpoint must resolve.
func TestDiscoverFallsBackToFileWSPath(t *testing.T) {
	noEnvCDP(t)
	// (a) 404-to-everything server: the ws path is NOT a live DevTools
	// endpoint, so discovery must refuse (a squatter would answer the same).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	prof := t.TempDir()
	os.Symlink(fmt.Sprintf("h-%d", os.Getpid()), filepath.Join(prof, "SingletonLock"))
	os.WriteFile(filepath.Join(prof, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port)+"\n/devtools/browser/fromfile\n"), 0o644)
	if ws, _, err := discoverProfileWS(context.Background(), prof); err == nil || ws != "" {
		t.Fatalf("404-only endpoint must be refused, got ws=%q err=%v", ws, err)
	}

	// (b) /json/version 404s but the ws path answers the upgrade → trusted.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port2 := ln2.Addr().(*net.TCPAddr).Port
	srv2 := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/devtools/browser/") {
			// Minimal 101 handshake response — no ws dependency needed.
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
			buf.Flush()
			return
		}
		http.NotFound(w, r) // /json/* disabled, Chrome 147+ style
	})}
	go srv2.Serve(ln2)
	defer srv2.Close()
	prof2 := t.TempDir()
	os.Symlink(fmt.Sprintf("h-%d", os.Getpid()), filepath.Join(prof2, "SingletonLock"))
	os.WriteFile(filepath.Join(prof2, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port2)+"\n/devtools/browser/fromfile\n"), 0o644)
	ws, _, err := discoverProfileWS(context.Background(), prof2)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/fromfile", port2); ws != want {
		t.Fatalf("got %q want %q", ws, want)
	}
}

// 403 from /json/version = Chrome 144+ permission popup → ErrPermissionBlocked.
func TestDiscoverPermissionBlocked(t *testing.T) {
	noEnvCDP(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	prof := filepath.Join(home, ".config", "google-chrome")
	os.MkdirAll(prof, 0o755)
	os.Symlink(fmt.Sprintf("h-%d", os.Getpid()), filepath.Join(prof, "SingletonLock"))
	os.WriteFile(filepath.Join(prof, "DevToolsActivePort"), []byte(strconv.Itoa(port)+"\n/devtools/browser/x\n"), 0o644)

	// Exercise discoverProfileWS directly: a full DiscoverLiveWS scan is
	// platform-dependent (profileDirs) and picks up ambient debug ports,
	// neither of which this unit test wants.
	_, _, err = discoverProfileWS(context.Background(), prof)
	if err == nil || !strings.Contains(err.Error(), "permission-blocked") {
		t.Fatalf("want permission-blocked, got %v", err)
	}
}

// Explicit endpoints beat the scan.
func TestDiscoverExplicitEndpoints(t *testing.T) {
	t.Setenv("WHIP_CDP_WS", "ws://example:1234/devtools/browser/explicit")
	ws, err := DiscoverLiveWS(context.Background())
	if err != nil || ws != "ws://example:1234/devtools/browser/explicit" {
		t.Fatalf("WHIP_CDP_WS: got %q %v", ws, err)
	}
}

// --- SSRF floor (url_safety.py port) ---

func TestCheckURLFloor(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://metadata.google.internal/",
		"http://169.254.170.2/v2/metadata",
	} {
		if err := CheckURL(ctx, u); err == nil {
			t.Errorf("%s must be blocked", u)
		}
	}
	if err := CheckURL(ctx, "https://example.com/"); err != nil {
		t.Errorf("example.com must pass: %v", err)
	}
	if err := CheckURL(ctx, "chrome://newtab"); err != nil {
		t.Errorf("non-http schemes pass: %v", err)
	}
}

func TestCheckPrivateURL(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"http://127.0.0.1:8080/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://100.64.1.2/", // CGNAT
		"http://[::1]/",
	} {
		if err := CheckPrivateURL(ctx, u, false); err == nil {
			t.Errorf("%s must be blocked", u)
		}
	}
	if err := CheckPrivateURL(ctx, "https://example.com/", false); err != nil {
		t.Errorf("example.com must pass: %v", err)
	}
}

// --- Session/mode selection ---

func TestSessionModeSelection(t *testing.T) {
	m := NewManager(ModeLive)
	s, err := m.Session("")
	if err != nil || s.name != "default" || s.mode != ModeLive {
		t.Fatalf("default session: %+v %v", s, err)
	}
	s, err = m.Session("headless:scrape")
	if err != nil || s.name != "scrape" || s.mode != ModeHeadless {
		t.Fatalf("mode-prefixed session: %+v %v", s, err)
	}
	if _, err := m.Session("bogus-mode:x"); err == nil {
		t.Fatal("unknown mode prefix must fail")
	}
	if _, err = m.Session("../evil"); err == nil {
		t.Fatal("path-traversal name must fail")
	}
	// Same key reuses the session; different modes don't collide.
	a, _ := m.Session("work")
	b, _ := m.Session("work")
	if a != b {
		t.Fatal("same name must return the same session")
	}
	c, _ := m.Session("dedicated:work")
	if c == a {
		t.Fatal("different modes must not share a session")
	}
}

// --- hermes-style auto-launch fallback (browser_connect.py port) ---

// portLive must see a browser that bound [::1] only (a squatter on the v4
// loopback pushes Chrome there) — hermes's dual-stack discovery.
func TestPortLiveDualStack(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback here: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if !portLive(port) {
		t.Fatalf("portLive missed a [::1]-only listener on %d", port)
	}
}

// A non-Chrome HTTP server squatting the debug port (the node-on-9222
// failure class) answers 404 HTML to /json/version; discovery must not
// trust it even when a profile file points there.
func TestSquatterRejected(t *testing.T) {
	noEnvCDP(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // Express-style junk, not Chrome's /json/version
	})}
	go srv.Serve(ln)
	defer srv.Close()

	// Direct: no wsPath to trust → error, never a bogus ws:// URL.
	if ws, err := resolveWSURL(context.Background(), "", "", port, ""); err == nil || ws != "" {
		t.Fatalf("squatter must not resolve: ws=%q err=%v", ws, err)
	}

	// Via the profile scan: the file's ws path belongs to a dead browser
	// (the squatter holds the port now), so the scan must skip the profile
	// and end at ErrNoLiveBrowser.
	home := t.TempDir()
	t.Setenv("HOME", home)
	prof := filepath.Join(home, ".config", "google-chrome")
	os.MkdirAll(prof, 0o755)
	os.Symlink(fmt.Sprintf("h-%d", os.Getpid()), filepath.Join(prof, "SingletonLock"))
	os.WriteFile(filepath.Join(prof, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port)+"\n/devtools/browser/dead\n"), 0o644)
	if ws, ok := DiscoverWSForProfile(context.Background(), prof); ok || ws != "" {
		t.Fatalf("squatter behind profile file must not reattach: %q", ws)
	}
}

// DiscoverWSForProfile round-trips a live profile: file + lock + a
// Chrome-shaped /json/version → WS URL.
func TestDiscoverWSForProfileLive(t *testing.T) {
	noEnvCDP(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			fmt.Fprintf(w, `{"Browser":"HeadlessChrome/150.0","webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/browser/live"}`, port)
			return
		}
		http.NotFound(w, r)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	prof := t.TempDir()
	os.Symlink(fmt.Sprintf("h-%d", os.Getpid()), filepath.Join(prof, "SingletonLock"))
	os.WriteFile(filepath.Join(prof, "DevToolsActivePort"),
		[]byte(strconv.Itoa(port)+"\n/devtools/browser/live\n"), 0o644)
	ws, ok := DiscoverWSForProfile(context.Background(), prof)
	if !ok || !strings.Contains(ws, "/devtools/browser/live") {
		t.Fatalf("reattach discovery: %q ok=%v", ws, ok)
	}

	// Absent/dead profile: not ok.
	if _, ok := DiscoverWSForProfile(context.Background(), filepath.Join(prof, "nope")); ok {
		t.Fatal("missing profile must not reattach")
	}
}

// --- Session notice + fallback orchestration ---

// fakeBackend implements Backend with no browser behind it — session tests
// substitute it via the openNamed hook.
type fakeBackend struct {
	mode     Mode
	obtained Obtained
	closed   bool
}

func (f *fakeBackend) Info(context.Context) (PageInfo, error)          { return PageInfo{}, nil }
func (f *fakeBackend) Navigate(context.Context, string) error          { return nil }
func (f *fakeBackend) Back(context.Context) error                      { return nil }
func (f *fakeBackend) Eval(context.Context, string) (string, error)    { return "null", nil }
func (f *fakeBackend) ClickAt(context.Context, float64, float64) error { return nil }
func (f *fakeBackend) TypeText(context.Context, string) error          { return nil }
func (f *fakeBackend) PressKey(context.Context, string) error          { return nil }
func (f *fakeBackend) Fill(context.Context, string, string) error      { return nil }
func (f *fakeBackend) Scroll(context.Context, float64) error           { return nil }
func (f *fakeBackend) WaitLoad(context.Context) error                  { return nil }
func (f *fakeBackend) WaitElement(context.Context, string, bool) (bool, error) {
	return true, nil
}
func (f *fakeBackend) Screenshot(context.Context, int) ([]byte, error) { return nil, nil }
func (f *fakeBackend) AXTree(context.Context) (string, error)          { return "[]", nil }
func (f *fakeBackend) BoxModel(context.Context, int) (float64, float64, error) {
	return 0, 0, nil
}
func (f *fakeBackend) Tabs(context.Context) ([]Tab, error)  { return nil, nil }
func (f *fakeBackend) UseTab(context.Context, string) error { return nil }
func (f *fakeBackend) UploadFiles(context.Context, string, []string) error {
	return nil
}
func (f *fakeBackend) HandleDialog(bool, string) error { return nil }
func (f *fakeBackend) Mode() Mode                      { return f.mode }
func (f *fakeBackend) Obtained() Obtained              { return f.obtained }
func (f *fakeBackend) Close() error                    { f.closed = true; return nil }

func testManager(t *testing.T, mode Mode, fn func(context.Context, Mode, string) (Backend, error)) *Manager {
	t.Helper()
	m := NewManager(mode)
	m.open = func(ctx context.Context, mode Mode, name, _ string, _ []string, _ *capability.ProcessManager, _ string) (Backend, error) {
		return fn(ctx, mode, name)
	}
	return m
}

// A live-mode session that fell back reports the notice once, prepended to
// the first output; a real live attach and explicit dedicated sessions stay
// silent.
func TestSessionFallbackNotice(t *testing.T) {
	ctx := context.Background()
	lines := func(s *Session, n int) []string {
		var out []string
		for range n {
			o, err := s.Do(ctx, func(Backend) (string, error) { return "result", nil })
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, o)
		}
		return out
	}

	m := testManager(t, ModeLive, func(_ context.Context, m Mode, _ string) (Backend, error) {
		return &fakeBackend{mode: m, obtained: ObtainedLaunched}, nil
	})
	s, err := m.Session("")
	if err != nil {
		t.Fatal(err)
	}
	got := lines(s, 2)
	if !strings.HasPrefix(got[0], fallbackNotice) {
		t.Fatalf("first output must carry the notice: %q", got[0])
	}
	if strings.Contains(got[1], "Note:") {
		t.Fatalf("notice must fire once, second output: %q", got[1])
	}

	// Explicit dedicated session: user asked for this browser — no notice.
	sd, err := m.Session("dedicated:quiet")
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(sd, 1)[0]; got != "result" {
		t.Fatalf("dedicated session must be silent: %q", got)
	}

	// Live attach that actually attached: no notice.
	m2 := testManager(t, ModeLive, func(_ context.Context, m Mode, _ string) (Backend, error) {
		return &fakeBackend{mode: m, obtained: ObtainedLive}, nil
	})
	s2, _ := m2.Session("")
	if got := lines(s2, 1)[0]; got != "result" {
		t.Fatalf("real live attach must be silent: %q", got)
	}
}

// A reopened backend (after a connection drop) re-arms the notice when it
// falls back again.
func TestSessionNoticeRearmedOnReopen(t *testing.T) {
	ctx := context.Background()
	m := testManager(t, ModeLive, func(_ context.Context, m Mode, _ string) (Backend, error) {
		return &fakeBackend{mode: m, obtained: ObtainedLaunched}, nil
	})
	s, _ := m.Session("")
	if _, err := s.Do(ctx, func(Backend) (string, error) { return "r1", nil }); err != nil {
		t.Fatal(err)
	}
	s.drop() // simulate a dead connection being dropped
	out, err := s.Do(ctx, func(Backend) (string, error) { return "r2", nil })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, fallbackNotice) {
		t.Fatalf("reopened fallback must re-notify: %q", out)
	}
}

func TestManagersKeepLaunchOptionsIsolated(t *testing.T) {
	t.Setenv("WHIP_BROWSER_DRIVER", "")
	type launch struct {
		driver string
		env    string
	}
	launches := make(chan launch, 2)
	open := func(_ context.Context, mode Mode, _, driver string, env []string, _ *capability.ProcessManager, _ string) (Backend, error) {
		launches <- launch{driver: driver, env: strings.Join(env, ",")}
		return &fakeBackend{mode: mode, obtained: ObtainedLive}, nil
	}
	one, two := NewManager(ModeHeadless), NewManager(ModeHeadless)
	one.open, two.open = open, open
	one.SwitchDriver(DriverChromedp)
	one.SetEnvironment([]string{"SESSION=one"})
	two.SetEnvironment([]string{"SESSION=two"})
	for _, manager := range []*Manager{one, two} {
		session, err := manager.Session("")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Do(t.Context(), func(Backend) (string, error) { return "ok", nil }); err != nil {
			t.Fatal(err)
		}
	}
	got := map[launch]bool{<-launches: true, <-launches: true}
	if !got[launch{DriverChromedp, "SESSION=one"}] || !got[launch{DriverRod, "SESSION=two"}] {
		t.Fatalf("launch options crossed managers: %v", got)
	}
}

func TestManagerScopesLaunchesByProcessRoot(t *testing.T) {
	manager := NewManager(ModeDedicated)
	processes := capability.NewProcessManager()
	t.Cleanup(func() { _ = processes.Close() })
	var name, root string
	manager.open = func(_ context.Context, mode Mode, sessionName, _ string, _ []string, got *capability.ProcessManager, rootID string) (Backend, error) {
		if got != processes {
			t.Fatal("process manager was not forwarded")
		}
		name, root = sessionName, rootID
		return &fakeBackend{mode: mode}, nil
	}
	manager.SetProcessOptions(processes, "root-one", []string{"PATH=/bin"})
	session, err := manager.Session("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Do(t.Context(), func(Backend) (string, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if name != "root-one-default" || root != "root-one" {
		t.Fatalf("launch name=%q root=%q", name, root)
	}
}

// A squatter on the fallback port must not be mistaken for a browser:
// DiscoverLiveWS should end at ErrNoLiveBrowser (which Open then turns into
// a launched dedicated Chrome), never a bogus ws URL.
func TestPortProbeSquatterTriggersFallback(t *testing.T) {
	if os.Getenv("WHIP_SKIP_PORT_SQUATTER_TEST") == "1" {
		// A REAL Chrome on a probe port (common on dev machines, 9223 via
		// --remote-debugging-port) makes this test fail for environmental
		// reasons; the pre-commit hook sets the skip so local commits work.
		t.Skip("skipped via WHIP_SKIP_PORT_SQUATTER_TEST")
	}
	// Squat both conventional ports with non-Chrome HTTP servers.
	var lns []net.Listener
	for _, p := range []int{9222, 9223} {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			continue // port busy — fine, that IS a squatter
		}
		lns = append(lns, ln)
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r) // not Chrome
		})}
		go srv.Serve(ln)
		defer srv.Close()
	}
	if len(lns) == 0 {
		t.Skip("could not bind 9222/9223")
	}
	noEnvCDP(t)
	t.Setenv("HOME", t.TempDir())

	ws, err := DiscoverLiveWS(context.Background())
	if err == nil {
		t.Fatalf("squatter must not resolve to a ws url: %q", ws)
	}
	if !errors.Is(err, ErrNoLiveBrowser) {
		t.Fatalf("want ErrNoLiveBrowser (triggers fallback), got: %v", err)
	}
}
