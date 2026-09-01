// Package browser provides whip's native browser subsystem: one Go binary
// driving the user's live Chrome (their real cookies/sessions), a dedicated
// whip-owned Chrome, or a headless instance — no Python, no Node.
//
// Design: docs/learnings/browser-use-integration.md §5b. The public surface
// is the Backend interface with go-rod/rod behind it; chromedp is a drop-in
// backup if rod ever stalls. The live-attach discovery is a port of
// browser-harness's daemon.py (profile scan, DevToolsActivePort,
// SingletonLock liveness, Chrome 144+ permission-popup errors).
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/context-labs/whip/internal/capability"
)

// Mode selects which browser whip drives.
type Mode string

const (
	// ModeLive attaches to the user's running everyday browser via its
	// CDP endpoint — their real cookies and logins. Discovered via profile
	// scan; never launches or closes the browser.
	ModeLive Mode = "live"
	// ModeDedicated launches a separate Chrome instance with a whip-owned
	// user-data-dir (~/.whip/browser/dedicated-profile[-<session>]) and remote
	// debugging enabled from the start — no permission popups.
	ModeDedicated Mode = "dedicated"
	// ModeHeadless is ModeDedicated without a window.
	ModeHeadless Mode = "headless"
	// ModeExtension drives the user's real, logged-in Chrome tab through the
	// whip extension (chrome.debugger CDP tunnel via extrelay). The only way
	// to drive the default profile on Chrome ≥ 136, where direct CDP is
	// blocked. Requires the unpacked extension loaded + a tab pinned via the
	// toolbar icon (`whip browser install` sets it up).
	ModeExtension Mode = "extension"
)

// ErrPermissionBlocked reports Chrome 144+'s per-connection "Allow remote
// debugging?" popup (or the chrome://inspect toggle being off) standing
// between whip and a live browser. The user must act in Chrome; retry
// after they confirm.
var ErrPermissionBlocked = errors.New("chrome permission-blocked")

// ErrNoLiveBrowser means no supported Chromium-family browser with a
// reachable debug port was found running.
var ErrNoLiveBrowser = errors.New("no live browser with remote debugging found")

// errDetachFailed reports that Close could not sever the CDP socket — a rod
// upgrade changed the unexported layout detach() reflects into. The browser
// connection leaks rather than being torn down; loud so it's not silent.
var errDetachFailed = errors.New("detach failed: rod internal layout changed")

// Backend is the browser driver contract. *Browser implements it with rod;
// tests substitute fakes, and chromedp is the drop-in backup (§5b).
type Backend interface {
	// Info reports the attached page's URL, title, viewport, and scroll
	// position, or the pending native JS dialog when one is open.
	Info(ctx context.Context) (PageInfo, error)
	// Navigate loads url in the controlled tab (creating it if needed) and
	// waits for load.
	Navigate(ctx context.Context, url string) error
	// Back steps the tab history.
	Back(ctx context.Context) error
	// Eval evaluates a JS expression in the tab and returns the JSON value.
	Eval(ctx context.Context, expression string) (string, error)
	// ClickAt dispatches trusted mouse events at viewport coordinates.
	ClickAt(ctx context.Context, x, y float64) error
	// TypeText inserts text into the focused element.
	TypeText(ctx context.Context, text string) error
	// PressKey sends a named key (Enter, Tab, Backspace, ArrowDown, …) with
	// real key events so framework listeners fire.
	PressKey(ctx context.Context, key string) error
	// Fill focuses a selector, clears it, and types — safe for
	// React/Vue-controlled inputs.
	Fill(ctx context.Context, selector, text string) error
	// Scroll dispatches a mouse-wheel event at the viewport center.
	Scroll(ctx context.Context, dy float64) error
	// WaitLoad blocks until document.readyState is complete or ctx expires.
	WaitLoad(ctx context.Context) error
	// WaitElement polls for a selector; visible additionally requires layout.
	WaitElement(ctx context.Context, selector string, visible bool) (bool, error)
	// Screenshot returns a JPEG of the viewport, downscaled so no side
	// exceeds maxDim (0 = no limit) — sized for direct use as a vision
	// image part.
	Screenshot(ctx context.Context, maxDim int) (jpeg []byte, err error)
	// AXTree returns the page's accessibility tree as compact JSON
	// (role/name/backendDOMNodeId per node). Large — callers filter.
	AXTree(ctx context.Context) (string, error)
	// BoxModel returns the viewport-px center of the node, for AXTree →
	// ClickAt workflows.
	BoxModel(ctx context.Context, backendNodeID int) (x, y float64, err error)
	// Tabs lists open page targets.
	Tabs(ctx context.Context) ([]Tab, error)
	// UseTab switches control to the given target id.
	UseTab(ctx context.Context, targetID string) error
	// UploadFiles sets files on a file input matched by selector.
	UploadFiles(ctx context.Context, selector string, paths []string) error
	// Close releases resources. Live attach and dedicated detach (the browser
	// stays alive as a reattach target; rod's Leakless guardian reaps a
	// launched dedicated Chrome when the agent process exits). Headless kills
	// its process — there's no reattach value in a windowless browser.
	Close() error
	// Mode returns the session's browser mode (for mode-dependent policy).
	Mode() Mode
	// Obtained reports how the connection was established: attached to the
	// user's live browser, freshly launched, or reattached to a running
	// whip Chrome. Live-mode sessions that fell back report launched, so
	// the session layer can tell the model which browser it's driving.
	Obtained() Obtained
	// HandleDialog accepts or dismisses the next pending native JS dialog,
	// blocking briefly for one to appear.
	HandleDialog(accept bool, promptText string) error
}

// PageInfo mirrors browser-harness's page_info() helper.
type PageInfo struct {
	URL, Title            string
	Width, Height         int
	ScrollX, ScrollY      float64
	PageWidth, PageHeight float64
	Dialog                *Dialog `json:",omitempty"`
}

// Dialog is a pending native JS dialog (alert/confirm/prompt/beforeunload).
// The page's JS thread is frozen until HandleDialog responds.
type Dialog struct {
	Type, Message, DefaultPrompt string
}

// Tab is one open page target.
type Tab struct {
	TargetID, Title, URL string
}

// Browser owns one rod browser connection plus the controlled page.
type Browser struct {
	mode       Mode
	browser    *rod.Browser
	page       *rod.Page
	launcher   *launcher.Launcher // non-nil only when we launched (dedicated/headless)
	obtained   Obtained           // how this connection was established
	profileDir string             // dedicated/headless profile (reattach target)
	unregister func()             // removes a dependency-owned launch from root tracking
}

// Obtained records how a Backend connection came about — the session layer
// reports a live→launched fallback once per session.
type Obtained int

const (
	ObtainedLive Obtained = iota
	ObtainedLaunched
	ObtainedReattached
)

// Driver names for the browser subsystem's two implementations.
const (
	DriverRod      = "rod"      // default — battle-tested here
	DriverChromedp = "chromedp" // the spike fallback (chromedp-spike branch)
)

// Drivers lists the selectable drivers for UI pickers.
func Drivers() []string { return []string{DriverRod, DriverChromedp} }

// DefaultDriver returns the environment-pinned driver or rod.
func DefaultDriver() string {
	if d := os.Getenv("WHIP_BROWSER_DRIVER"); d != "" {
		return d
	}
	return "rod"
}

// Open connects (or launches) per mode and attaches to a controllable tab.
// Attach mode discovery errors are actionable (ErrNoLiveBrowser,
// ErrPermissionBlocked) — surface them to the user, not the model.
// Returns the Backend selected by Driver. Named sessions get isolated
// dedicated/headless profiles (a shared profile dir deadlocks concurrent
// sessions on SingletonLock — caught by TestConcurrentSessions).
func Open(ctx context.Context, mode Mode) (Backend, error) {
	return OpenNamed(ctx, mode, "default")
}

// OpenNamed is Open with a session name for profile isolation. Extension
// mode ignores the name (one pinned tab per relay) and always uses rod — the
// relay speaks CDP, and chromedp is only the spike backup for local launches.
func OpenNamed(ctx context.Context, mode Mode, sessionName string) (Backend, error) {
	return openNamedWithOptions(ctx, mode, sessionName, DefaultDriver(), nil, nil, "")
}

func openNamedWithOptions(ctx context.Context, mode Mode, sessionName, driver string, env []string, processes *capability.ProcessManager, rootID string) (Backend, error) {
	if mode == ModeExtension {
		return openExtension(ctx)
	}
	if driver == DriverChromedp {
		return openChromedp(ctx, mode, sessionName, env)
	}
	return openRod(ctx, mode, sessionName, env, processes, rootID)
}

// openRod is the rod-backed Open (the default driver).
//
// Live mode falls back hermes-style (/browser connect): when no debuggable
// browser is found (ErrNoLiveBrowser — includes a non-Chrome squatter on
// the debug port), whip launches its dedicated Chrome for this session
// instead of dead-ending the tool call. ErrPermissionBlocked still surfaces
// — only the user can click Chrome's Allow popup. Dedicated/headless
// reattach to an already-running whip Chrome for the same profile rather
// than spawning a duplicate.
func openRod(ctx context.Context, mode Mode, sessionName string, env []string, processes *capability.ProcessManager, rootID string) (*Browser, error) {
	b := &Browser{mode: mode}
	switch mode {
	case ModeLive:
		ws, err := DiscoverLiveWS(ctx)
		if err != nil {
			if !errors.Is(err, ErrNoLiveBrowser) {
				return nil, err
			}
			// Fallback: launch the dedicated Chrome. Keep the original
			// discovery error's guidance if the fallback also fails.
			fb, ferr := openRod(ctx, ModeDedicated, sessionName, env, processes, rootID)
			if ferr != nil {
				return nil, fmt.Errorf("%w; dedicated fallback failed: %w", err, ferr)
			}
			return fb, nil
		}
		b.browser = rod.New().ControlURL(ws)
		if err := b.browser.Connect(); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "403") || strings.Contains(msg, "permission") || strings.Contains(msg, "timed out") {
				return nil, fmt.Errorf("%w: Chrome's 'Allow remote debugging?' popup needs a click (or enable chrome://inspect/#remote-debugging): %s", ErrPermissionBlocked, msg)
			}
			return nil, fmt.Errorf("connect to live browser: %w", err)
		}
		b.obtained = ObtainedLive
	case ModeDedicated, ModeHeadless:
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		profileDir := dedicatedProfileDir(home, sessionName)
		b.profileDir = profileDir
		// Reattach to a still-running whip Chrome for this profile (the
		// prior backend died or was closed without killing the browser).
		if ws, ok := DiscoverWSForProfile(ctx, profileDir); ok {
			b.browser = rod.New().ControlURL(ws)
			if err := b.browser.Connect(); err == nil {
				b.obtained = ObtainedReattached
				break
			}
			b.browser = nil // stale endpoint — fall through to a fresh launch
		}
		newLauncher := func() *launcher.Launcher {
			l := launcher.New().
				UserDataDir(profileDir).
				Set("remote-debugging-port", "0"). // random free port — a squatter on 9222 is irrelevant
				Leakless(true).
				Headless(mode == ModeHeadless)
			if env != nil {
				l = l.Env(env...)
			}
			if os.Getenv("WHIP_BROWSER_DEBUG") != "" {
				l = l.Set("enable-logging", "stderr").Set("v", "1")
			}
			if bin := os.Getenv("ROD_BROWSER_BIN"); bin != "" { // test/override hook
				l = l.Bin(bin)
			}
			return l
		}
		l, ws, err := launchDedicated(ctx, profileDir, newLauncher)
		if err != nil {
			return nil, err
		}
		b.launcher = l
		if processes != nil {
			b.unregister, err = processes.RegisterStop(rootID, func() error { return stopLauncher(l) })
			if err != nil {
				_ = stopLauncher(l)
				return nil, err
			}
		}
		b.obtained = ObtainedLaunched
		b.browser = rod.New().ControlURL(ws)
		if err := b.browser.Connect(); err != nil {
			l.Kill()    // unblock Cleanup: a healthy Chrome won't exit on its own
			l.Cleanup() // remove the failed profile + process tree
			if b.unregister != nil {
				b.unregister()
			}
			return nil, fmt.Errorf("connect to launched chrome: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown browser mode %q", mode)
	}
	b.browser = b.browser.Context(ctx)
	if err := b.attachPage(); err != nil {
		// detach, never b.browser.Close() — for a live/reattached browser we
		// don't own the process, and Browser.close would kill a Chrome another
		// backend (or the user) is driving.
		detach(b.browser)
		if b.launcher != nil {
			b.launcher.Kill()
			b.launcher.Cleanup()
			if b.unregister != nil {
				b.unregister()
			}
		}
		return nil, err
	}
	return b, nil
}

func stopLauncher(l *launcher.Launcher) error {
	l.Kill()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-l.PID(), 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("browser process group %d did not stop", l.PID())
}

// launchDedicated launches a dedicated/headless Chrome, recovering from a
// wedged prior instance. On launch failure it first kills any *live* Chrome
// still holding the profile (the detach-on-Close design intentionally leaves
// one behind), and only quarantines the profile dir when it's genuinely dead
// — renaming a live profile out from under a running Chrome would orphan its
// cookies/logins.
func launchDedicated(ctx context.Context, profileDir string, newLauncher func() *launcher.Launcher) (*launcher.Launcher, string, error) {
	l := newLauncher()
	ws, err := l.Launch()
	if err == nil {
		return l, ws, nil
	}
	if killProfileChromeQuiet(ctx, profileDir) {
		if l2 := newLauncher(); l2 != nil {
			if ws2, err2 := l2.Launch(); err2 == nil {
				return l2, ws2, nil
			}
		}
	}
	quarantine := profileDir + ".stale-" + time.Now().Format("20060102150405")
	if rerr := os.Rename(profileDir, quarantine); rerr != nil {
		return nil, "", fmt.Errorf("launch dedicated chrome: %w", err)
	}
	l = newLauncher()
	ws, err = l.Launch()
	if err != nil {
		return nil, "", fmt.Errorf("launch dedicated chrome after profile quarantine: %w", err)
	}
	return l, ws, nil
}

// killProfileChromeQuiet shuts down the Chrome behind a profile dir via its
// debug port (proto.BrowserClose), best-effort. Reports whether a live
// browser was found and told to close.
func killProfileChromeQuiet(ctx context.Context, profileDir string) bool {
	ws, ok := DiscoverWSForProfile(ctx, profileDir)
	if !ok {
		return false
	}
	b := rod.New().ControlURL(ws)
	if err := b.Context(ctx).Connect(); err != nil {
		return false
	}
	_ = b.Close() // Browser.close — intended: shut the wedged holder down
	time.Sleep(300 * time.Millisecond)
	return true
}

// internalURL matches browser-harness's INTERNAL prefix set.
func internalURL(u string) bool {
	for _, p := range []string{"chrome://", "chrome-untrusted://", "devtools://", "chrome-extension://", "about:"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

// attachPage picks the controllable tab: the first real page, else a
// reusable blank/new-tab page, else a fresh about:blank (daemon.py's
// attach_first_page, simplified: whip owns the whole connection so
// parallel agents fight over nothing — one Browser per caller).
func (b *Browser) attachPage() error {
	pages, err := b.browser.Pages()
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}
	var blank *rod.Page
	for _, p := range pages {
		info, err := p.Info()
		if err != nil {
			continue
		}
		if info.Type != "page" {
			continue
		}
		if !internalURL(info.URL) {
			b.page = p
			break
		}
		if blank == nil && (info.URL == "about:blank" || strings.HasPrefix(info.URL, "chrome://newtab") || strings.HasPrefix(info.URL, "edge://newtab")) {
			blank = p
		}
	}
	if b.page == nil {
		b.page = blank
	}
	if b.page == nil {
		b.page, err = b.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return fmt.Errorf("create tab: %w", err)
		}
	}
	// ponytail: browser-harness enables Page/DOM/Runtime/Network up front;
	// rod enables domains lazily per call. Skip the explicit enable (it
	// deadlocked against headless-shell's event stream in e2e).
	return nil
}

// Mode returns the session's browser mode (for mode-dependent policy).
func (b *Browser) Mode() Mode { return b.mode }

// Obtained reports how this connection was established (Backend).
func (b *Browser) Obtained() Obtained { return b.obtained }

// Close releases the connection; launched browsers are killed.
func (b *Browser) Close() error {
	if b.browser == nil {
		return nil
	}
	var err error
	switch {
	case b.mode == ModeLive || b.obtained == ObtainedReattached:
		// We don't own the process (the user's browser, or a dedicated
		// Chrome another backend launched): detach only — rod's Close sends
		// Browser.close, which would kill a browser we must not touch.
		if !detach(b.browser) {
			err = errDetachFailed
		}
	case b.mode == ModeDedicated:
		// Launched dedicated Chrome: detach, leaving it alive so a later
		// Open reattaches (the auto-launch fallback's reuse path) instead of
		// paying a fresh launch per call. The process exits with the agent
		// via the Leakless pid-guardian (launcher.Cleanup is only for the
		// launch-failure error paths, which kill explicitly).
		if !detach(b.browser) {
			err = errDetachFailed
		}
	default: // headless: no reattach value, no window — kill outright.
		err = b.browser.Close()
		if b.launcher != nil {
			b.launcher.Kill()
			b.launcher.Cleanup()
			if b.unregister != nil {
				b.unregister()
			}
		}
	}
	b.browser = nil
	return err
}

// --- Backend implementation ---

func (b *Browser) Info(ctx context.Context) (PageInfo, error) {
	if d, err := b.pendingDialog(ctx); err == nil && d != nil {
		return PageInfo{Dialog: d}, nil
	}
	res, err := b.runtimeEval(ctx, `JSON.stringify({url:location.href,title:document.title,w:innerWidth,h:innerHeight,sx:scrollX,sy:scrollY,pw:document.documentElement.scrollWidth,ph:document.documentElement.scrollHeight})`)
	if err != nil {
		return PageInfo{}, err
	}
	var raw struct {
		URL, Title     string
		W, H           int
		SX, SY, PW, PH float64
	}
	if err := json.Unmarshal([]byte(res.Result.Value.String()), &raw); err != nil {
		return PageInfo{}, err
	}
	return PageInfo{URL: raw.URL, Title: raw.Title, Width: raw.W, Height: raw.H, ScrollX: raw.SX, ScrollY: raw.SY, PageWidth: raw.PW, PageHeight: raw.PH}, nil
}

func (b *Browser) pendingDialog(ctx context.Context) (*Dialog, error) {
	// ponytail: browser-harness buffers Page.javascriptDialogOpening events on
	// a background reader; v1 surfaces dialogs only through HandleDialog when
	// an action hangs on one. Generalize to an event buffer if agents trip
	// on unexpected alerts.
	return nil, nil //nolint:nilnil // nil dialog = none pending; Info's `err == nil && d != nil` check relies on that contract
}

// HandleDialog accepts or dismisses the next pending native dialog,
// blocking up to 2s for one to appear.
func (b *Browser) HandleDialog(accept bool, promptText string) error {
	wait, handle := b.page.Timeout(2 * time.Second).HandleDialog()
	_ = wait()
	return handle(&proto.PageHandleJavaScriptDialog{Accept: accept, PromptText: promptText})
}

func (b *Browser) Navigate(ctx context.Context, url string) error {
	if b.page == nil {
		return errors.New("no attached tab")
	}
	p := b.page.Context(ctx)
	if err := p.Navigate(url); err != nil {
		return err
	}
	// Poll readyState via our raw-CDP eval instead of rod's WaitLoad (which
	// evals a rAF-loop helper that wedges against some page/server combos).
	return b.WaitLoad(ctx)
}

func (b *Browser) Back(ctx context.Context) error {
	p := b.page.Context(ctx)
	if err := p.NavigateBack(); err != nil {
		return err
	}
	return b.WaitLoad(ctx)
}

// Eval runs Runtime.evaluate directly (ByValue + AwaitPromise, per
// browser-harness's js()): the expression is evaluated as-is, with an
// IIFE-retry when Chrome reports an illegal top-level return — both
// "document.title" and "const x = 1; return x" work. rod's page.Eval wraps
// the snippet as a function body, which breaks bare expressions.
func (b *Browser) Eval(ctx context.Context, expression string) (string, error) {
	res, err := b.runtimeEval(ctx, expression)
	if err != nil && strings.Contains(err.Error(), "Illegal return statement") {
		res, err = b.runtimeEval(ctx, "(function(){"+expression+"})()")
	}
	if err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil || (res.Result.Subtype == "error") {
		desc := res.Result.Description
		if res.ExceptionDetails != nil && res.ExceptionDetails.Text != "" {
			desc = res.ExceptionDetails.Text + ": " + desc
		}
		return "", fmt.Errorf("JavaScript evaluation failed: %s; expression: %.160s", desc, expression)
	}
	if res.Result.Type == proto.RuntimeRemoteObjectTypeUndefined {
		return "null", nil
	}
	data, err := res.Result.Value.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *Browser) runtimeEval(ctx context.Context, expression string) (*proto.RuntimeEvaluateResult, error) {
	return proto.RuntimeEvaluate{
		Expression:    expression,
		ReturnByValue: true,
		AwaitPromise:  true,
	}.Call(b.page.Context(ctx))
}

func (b *Browser) ClickAt(ctx context.Context, x, y float64) error {
	m := proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMousePressed, X: x, Y: y, Button: proto.InputMouseButtonLeft, ClickCount: 1}
	if err := m.Call(b.page.Context(ctx)); err != nil {
		return err
	}
	m.Type = proto.InputDispatchMouseEventTypeMouseReleased
	return m.Call(b.page.Context(ctx))
}

func (b *Browser) TypeText(ctx context.Context, text string) error {
	return proto.InputInsertText{Text: text}.Call(b.page.Context(ctx))
}

// keyDefs maps key names to codes, per browser-harness's _KEYS table.
var keyDefs = map[string]struct {
	Code string
	Key  int
	Text string
}{
	"Enter":      {"Enter", 13, "\r"},
	"Tab":        {"Tab", 9, "\t"},
	"Backspace":  {"Backspace", 8, ""},
	"Escape":     {"Escape", 27, ""},
	"Delete":     {"Delete", 46, ""},
	" ":          {"Space", 32, " "},
	"ArrowLeft":  {"ArrowLeft", 37, ""},
	"ArrowUp":    {"ArrowUp", 38, ""},
	"ArrowRight": {"ArrowRight", 39, ""},
	"ArrowDown":  {"ArrowDown", 40, ""},
	"Home":       {"Home", 36, ""},
	"End":        {"End", 35, ""},
	"PageUp":     {"PageUp", 33, ""},
	"PageDown":   {"PageDown", 34, ""},
}

// PressKey sends keyDown(+char)+keyUp with virtual key codes so listeners
// checking e.keyCode / e.key both fire (helpers.py press_key).
func (b *Browser) PressKey(ctx context.Context, key string) error {
	p := b.page.Context(ctx)
	def, ok := keyDefs[key]
	if !ok && len(key) == 1 {
		def = struct {
			Code string
			Key  int
			Text string
		}{key, int(key[0]), key}
		ok = true
	}
	if !ok {
		return fmt.Errorf("unknown key %q", key)
	}
	down := proto.InputDispatchKeyEvent{
		Type: proto.InputDispatchKeyEventTypeKeyDown,
		Key:  key, Code: def.Code,
		WindowsVirtualKeyCode: def.Key, NativeVirtualKeyCode: def.Key,
	}
	if err := down.Call(p); err != nil {
		return err
	}
	if def.Text != "" {
		if err := (proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeChar, Text: def.Text, Key: key, Code: def.Code}).Call(p); err != nil {
			return err
		}
	}
	up := down
	up.Type = proto.InputDispatchKeyEventTypeKeyUp
	return up.Call(p)
}

// Fill clears and types into a framework-managed input with real key events
// plus synthetic input/change (helpers.py fill_input).
func (b *Browser) Fill(ctx context.Context, selector, text string) error {
	sel, _ := json.Marshal(selector)
	focused, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;e.focus();return true})()`, sel))
	if err != nil {
		return err
	}
	if focused != "true" {
		return fmt.Errorf("fill: element not found: %s", selector)
	}
	if err := b.PressKey(ctx, "Backspace"); err != nil { // select-all via eval is simpler and portable
		return err
	}
	if _, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return;const s=window.getSelection(),r=document.createRange();e.select&&e.select();r.selectNodeContents(e);s.removeAllRanges();s.addRange(r)})()`, sel)); err != nil {
		return err
	}
	if err := b.PressKey(ctx, "Backspace"); err != nil {
		return err
	}
	for _, ch := range text {
		if err := b.PressKey(ctx, string(ch)); err != nil {
			return err
		}
	}
	_, err = b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}))})()`, sel))
	return err
}

func (b *Browser) Scroll(ctx context.Context, dy float64) error {
	info, err := b.Info(ctx)
	if err != nil {
		return err
	}
	return (proto.InputDispatchMouseEvent{
		Type: proto.InputDispatchMouseEventTypeMouseWheel,
		X:    float64(info.Width) / 2, Y: float64(info.Height) / 2,
		DeltaX: 0, DeltaY: dy,
	}).Call(b.page.Context(ctx))
}

// WaitLoad polls document.readyState == complete (helpers.py wait_for_load)
// with a 15s cap when ctx has no earlier deadline.
func (b *Browser) WaitLoad(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		res, err := b.Eval(ctx, "document.readyState")
		if err == nil && res == `"complete"` {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// WaitElement polls for a selector, per helpers.py wait_for_element.
func (b *Browser) WaitElement(ctx context.Context, selector string, visible bool) (bool, error) {
	sel, _ := json.Marshal(selector)
	expr := fmt.Sprintf(`!!document.querySelector(%s)`, sel)
	if visible {
		expr = fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;if(typeof e.checkVisibility==='function')return e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});const s=getComputedStyle(e);return s.display!=='none'&&s.visibility!=='hidden'&&s.opacity!=='0'})()`, sel)
	}
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		res, err := b.Eval(ctx, expr)
		if err != nil {
			return false, err
		}
		if res == "true" {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-t.C:
		}
	}
}

// Screenshot captures the viewport as JPEG (quality 80), downscaled via the
// clip scale factor so no side exceeds maxDim — the vision-embed ladder from
// browser-use's _native_screenshot_result (1568px keeps under model caps).
func (b *Browser) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	quality := 80
	p := b.page.Context(ctx)
	req := &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatJpeg, Quality: &quality}
	if maxDim > 0 {
		metrics, err := proto.PageGetLayoutMetrics{}.Call(p)
		if err == nil && metrics.CSSLayoutViewport != nil {
			w := float64(metrics.CSSLayoutViewport.ClientWidth)
			h := float64(metrics.CSSLayoutViewport.ClientHeight)
			scale := 1.0
			if max(w, h) > float64(maxDim) {
				scale = float64(maxDim) / max(w, h)
			}
			req.Clip = &proto.PageViewport{X: 0, Y: 0, Width: w, Height: h, Scale: scale}
		}
	}
	shot, err := req.Call(p)
	if err != nil {
		return nil, err
	}
	return shot.Data, nil
}

// AXTree returns the full accessibility tree as compact JSON, filtered to
// the fields an agent needs (role, name, backendDOMNodeId) — per
// browser-harness's cdp("Accessibility.getFullAXTree") guidance, pre-filtered
// in Go instead of Python.
func (b *Browser) AXTree(ctx context.Context) (string, error) {
	res, err := proto.AccessibilityGetFullAXTree{}.Call(b.page.Context(ctx))
	if err != nil {
		return "", err
	}
	type node struct {
		Role          string `json:"role"`
		Name          string `json:"name"`
		BackendNodeID int    `json:"backendDOMNodeId"`
	}
	out := make([]node, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		if n.Ignored {
			continue
		}
		nn := node{BackendNodeID: int(n.BackendDOMNodeID)}
		if n.Role != nil {
			nn.Role = n.Role.Value.String()
		}
		if n.Name != nil {
			nn.Name = n.Name.Value.String()
		}
		if nn.Role == "" && nn.Name == "" {
			continue
		}
		out = append(out, nn)
	}
	data, err := json.Marshal(out)
	return string(data), err
}

// BoxModel returns the viewport-px center of a node's content quad.
func (b *Browser) BoxModel(ctx context.Context, backendNodeID int) (float64, float64, error) {
	res, err := proto.DOMGetBoxModel{BackendNodeID: proto.DOMBackendNodeID(backendNodeID)}.Call(b.page.Context(ctx))
	if err != nil {
		return 0, 0, err
	}
	q := res.Model.Content // [x1,y1,x2,y2,x3,y3,x4,y4]
	var sx, sy float64
	for i := range 4 {
		sx += q[i*2]
		sy += q[i*2+1]
	}
	return sx / 4, sy / 4, nil
}

func (b *Browser) Tabs(ctx context.Context) ([]Tab, error) {
	pages, err := b.browser.Context(ctx).Pages()
	if err != nil {
		return nil, err
	}
	var out []Tab
	for _, p := range pages {
		info, err := p.Info()
		if err != nil || info.Type != "page" {
			continue
		}
		out = append(out, Tab{TargetID: string(p.TargetID), Title: info.Title, URL: info.URL})
	}
	return out, nil
}

func (b *Browser) UseTab(ctx context.Context, targetID string) error {
	page, err := b.browser.Context(ctx).PageFromTarget(proto.TargetTargetID(targetID))
	if err != nil {
		return err
	}
	b.page = page
	return nil
}

// UploadFiles sets files on an <input type=file> (helpers.py upload_file).
func (b *Browser) UploadFiles(ctx context.Context, selector string, paths []string) error {
	p := b.page.Context(ctx)
	el, err := p.Element(selector)
	if err != nil {
		return fmt.Errorf("upload: element not found: %s", selector)
	}
	return el.SetFiles(paths)
}

// dedicatedProfileDir is the whip-owned profile dir for dedicated/headless
// sessions: one per named session (except "default", the shared one) so
// parallel sessions never collide on SingletonLock.
func dedicatedProfileDir(home, sessionName string) string {
	if sessionName == "" || sessionName == "default" {
		return filepath.Join(home, ".whip", "browser", "dedicated-profile")
	}
	return filepath.Join(home, ".whip", "browser", "dedicated-profile-"+sessionName)
}
