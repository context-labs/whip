package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/capability"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// NativeOptions is captured host configuration and process ownership. Profile
// is a host-derived safe ID, not an arbitrary client filesystem path.
type NativeOptions struct {
	Config                           browserconfig.Config
	Driver                           string
	Directory, Profile, ProcessOwner string
	Processes                        *capability.ProcessManager
	Authorize                        func(context.Context) (context.Context, func(), error)
}

// NativeConnection owns one exact transport and only the process it launched.
// Close detaches live Chrome, joins owned Chrome, and never reopens either one.
type NativeConnection struct {
	Backend
	wire               *nativeWire
	control            *Browser
	lifetime           context.Context
	cancel             context.CancelFunc
	process            *capability.Process
	processDone        chan struct{}
	relay              *extrelay.Relay
	extensionDirectory string
	once               sync.Once
	closeErr           error
}

func (c *NativeConnection) Lifetime() context.Context {
	if c.wire != nil {
		return c.wire.Lifetime()
	}
	return c.lifetime
}

func (c *NativeConnection) Close() error {
	c.once.Do(func() {
		c.cancel()
		if c.wire != nil {
			c.closeErr = c.wire.Close()
		}
		if c.process != nil {
			c.process.Stop()
			<-c.processDone
		}
		if c.relay != nil {
			c.closeErr = errors.Join(c.closeErr, c.relay.Close(), extrelay.RemoveNativeState(c.extensionDirectory, c.relay.Token()))
		}
	})
	return c.closeErr
}

func (c *NativeConnection) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	data, err := c.Backend.Screenshot(ctx, maxDim)
	if err != nil {
		return nil, err
	}
	if maxDim <= 0 {
		maxDim = 2048
	}
	return BoundJPEG(ctx, data, min(maxDim, 2048))
}

func (c *NativeConnection) HandleDialogContext(ctx context.Context, accept bool, text string) error {
	return (proto.PageHandleJavaScriptDialog{Accept: accept, PromptText: text}).Call(c.control.page.Context(ctx))
}

// OpenNative performs one authorized acquisition. Callers must commit durable
// dispatch before entering it. ctx bounds acquisition; lifetime owns reuse.
func OpenNative(ctx, lifetime context.Context, options NativeOptions) (result *NativeConnection, err error) {
	options.Config, err = options.Config.Normalize()
	if err != nil {
		return nil, err
	}
	if options.Config.Mode == "disabled" {
		return nil, errors.New("external browser control is disabled")
	}
	if options.Driver != DriverRod && options.Driver != DriverChromedp {
		return nil, errors.New("invalid external browser driver")
	}
	if options.Processes == nil || options.ProcessOwner == "" || !filepath.IsAbs(options.Directory) || !sessionNameRe.MatchString(options.Profile) {
		return nil, errors.New("explicit native browser ownership is required")
	}
	owned, cancel := context.WithCancel(lifetime)
	connection := &NativeConnection{lifetime: owned, cancel: cancel}
	defer func() {
		if err != nil {
			_ = connection.Close()
		}
	}()
	var endpoint string
	switch Mode(options.Config.Mode) {
	case ModeLive:
		endpoint, err = resolveNativeEndpoint(ctx, options.Config)
	case ModeDedicated, ModeHeadless:
		endpoint, err = connection.launch(ctx, options)
	case ModeExtension:
		connection.extensionDirectory = filepath.Join(options.Directory, "browser", "extension")
		connection.relay, err = extrelay.NewRelay()
		if err == nil {
			err = extrelay.WriteNativeState(connection.extensionDirectory, connection.relay.Addr(), connection.relay.Token())
		}
		if err == nil {
			err = connection.relay.WaitAttached(ctx)
		}
		if err == nil {
			endpoint = connection.relay.CDPURL()
		}
	}
	if err != nil {
		return nil, err
	}
	connection.wire, err = dialNativeWire(ctx, owned, endpoint)
	if err != nil {
		return nil, err
	}
	connection.wire.authorize = options.Authorize
	// Acquisition cancellation must interrupt driver initialization too. The
	// successful connection is subsequently owned only by its explicit lifetime.
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(interrupted); connection.wire.retire() })
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	rb := rod.New().Context(owned).Client(connection.wire).NoDefaultDevice()
	if err = rb.Connect(); err != nil {
		return nil, errors.New("external browser initialization failed")
	}
	base := &Browser{mode: Mode(options.Config.Mode), browser: rb, closeTransport: connection.Close}
	if err = base.attachPage(); err != nil {
		return nil, errors.New("external browser tab acquisition failed")
	}
	connection.control = base
	connection.Backend = base
	if options.Driver == DriverChromedp {
		executor := &desktopExecutor{ctx: owned, client: connection.wire, sessionID: string(base.page.SessionID)}
		connection.Backend = &nativeChromeDP{chromedpBackend: &chromedpBackend{mode: base.mode, executor: executor}, control: base, executor: executor}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return connection, nil
}

type nativeChromeDP struct {
	*chromedpBackend
	control  *Browser
	executor *desktopExecutor
}

func (b *nativeChromeDP) Close() error { return b.control.Close() }

func (b *nativeChromeDP) Tabs(ctx context.Context) ([]Tab, error) { return b.control.Tabs(ctx) }
func (b *nativeChromeDP) UseTab(ctx context.Context, target string) error {
	if err := b.control.UseTab(ctx, target); err != nil {
		return err
	}
	b.executor.sessionID = string(b.control.page.SessionID)
	return nil
}

func resolveNativeEndpoint(ctx context.Context, settings browserconfig.Config) (string, error) {
	if settings.LiveProfile != "" {
		return nativeProfileEndpoint(settings.LiveProfile)
	}
	if err := browserconfig.ValidateEndpoint(settings.LiveEndpoint); err != nil {
		return "", err
	}
	if strings.HasPrefix(settings.LiveEndpoint, "ws:") {
		return settings.LiveEndpoint, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(settings.LiveEndpoint, "/")+"/json/version", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return "", errors.New("configured live browser is unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusForbidden {
		return "", ErrPermissionBlocked
	}
	if response.StatusCode != http.StatusOK {
		return "", ErrNoLiveBrowser
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(data) > 8192 {
		return "", errors.New("live browser discovery exceeds bounds")
	}
	var value struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if json.Unmarshal(data, &value) != nil || browserconfig.ValidateEndpoint(value.URL) != nil || !strings.HasPrefix(value.URL, "ws:") {
		return "", errors.New("invalid live browser discovery")
	}
	expected, _ := url.Parse(settings.LiveEndpoint)
	actual, _ := url.Parse(value.URL)
	if actual.Host != expected.Host {
		return "", errors.New("live browser discovery changed endpoint authority")
	}
	return value.URL, nil
}

func nativeProfileEndpoint(profile string) (string, error) {
	root, err := os.OpenRoot(profile)
	if err != nil {
		return "", errors.New("configured browser profile is unavailable")
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat("DevToolsActivePort")
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrNoLiveBrowser
	}
	file, err := root.Open("DevToolsActivePort")
	if err != nil {
		return "", ErrNoLiveBrowser
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return "", errors.New("browser profile endpoint exceeds bounds")
	}
	port, path, err := parseDevToolsActivePort(data)
	if err != nil {
		return "", ErrNoLiveBrowser
	}
	endpoint := "ws://127.0.0.1:" + strconv.Itoa(port) + path
	if err := browserconfig.ValidateEndpoint(endpoint); err != nil {
		return "", ErrNoLiveBrowser
	}
	return endpoint, nil
}

func (c *NativeConnection) launch(ctx context.Context, options NativeOptions) (string, error) {
	root, err := os.OpenRoot(options.Directory)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	relative := filepath.Join("browser", "profiles", options.Profile)
	if err := root.MkdirAll(relative, 0o700); err != nil {
		return "", err
	}
	info, err := root.Lstat(relative)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("native browser profile is not a directory")
	}
	profile := filepath.Join(options.Directory, relative)
	if browserRunningForProfile(profile) {
		return "", errors.New("native browser profile is already in use; existing Chrome was left unchanged")
	}
	// Remove only this owned profile's stale discovery file. Never kill an
	// existing Chrome, move a profile, or retry a failed launch.
	if err := root.Remove(filepath.Join(relative, "DevToolsActivePort")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	args := []string{"--user-data-dir=" + profile, "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check", "about:blank"}
	// These profiles are private automation state, never an existing user profile.
	// Avoid consulting the user's Keychain (and its blocking permission prompt).
	if runtime.GOOS == "darwin" {
		args = append(args, "--use-mock-keychain")
	}
	if options.Config.Mode == "headless" {
		args = append(args, "--headless=new")
	}
	c.process, err = options.Processes.Start(c.lifetime, options.ProcessOwner, options.Config.Executable, args, capability.ProcessOptions{Cwd: profile, CwdIdentity: info, Stdin: bytes.NewReader(nil), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		return "", errors.New("native Chrome launch failed")
	}
	c.processDone = make(chan struct{})
	go func() { defer close(c.processDone); _ = c.process.Wait(); c.cancel() }()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if endpoint, err := nativeProfileEndpoint(profile); err == nil {
			target, readErr := os.Readlink(filepath.Join(profile, "SingletonLock"))
			if readErr == nil && strings.HasSuffix(target, "-"+strconv.Itoa(c.process.PID())) {
				return endpoint, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("native Chrome did not publish an owned endpoint: %w", ctx.Err())
		case <-c.processDone:
			return "", errors.New("native Chrome exited before publishing an owned endpoint")
		case <-ticker.C:
		}
	}
}
