package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser/extrelay"
	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/capability"
	"github.com/gobwas/ws"
)

func nativeFakeBrowser(t *testing.T) string {
	t.Helper()
	relay, err := extrelay.NewRelay()
	if err != nil {
		t.Fatal(err)
	}
	extension := dialRelayExt(t, relay)
	extension.send(t, `{"method":"whip.attached","params":{"tabId":7,"title":"Example","url":"https://example.com/"}}`)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := relay.WaitAttached(ctx); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	group.Go(func() { extension.answerLoop(t) })
	target, _ := url.Parse("http://" + relay.Addr())
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.URL.Path = "/cdp"
		r.Out.URL.RawQuery = "token=" + relay.Token()
	}}
	mux := http.NewServeMux()
	mux.Handle("/devtools/browser/test", proxy)
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"Browser": "Chrome/test", "webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/test"})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(func() { _ = relay.Close(); extension.close(); group.Wait(); server.Close() })
	return server.URL
}

func nativeOptions(t *testing.T, settings browserconfig.Config, driver string) NativeOptions {
	t.Helper()
	manager := capability.NewProcessManager()
	t.Cleanup(func() { _ = manager.Close() })
	return NativeOptions{Config: settings, Driver: driver, Directory: t.TempDir(), Profile: "owned-test", ProcessOwner: "browser-test", Processes: manager}
}

func TestNativeLiveBothDriversNoFallbackOrReplay(t *testing.T) {
	for _, driver := range []string{DriverRod, DriverChromedp} {
		t.Run(driver, func(t *testing.T) {
			endpoint := nativeFakeBrowser(t)
			options := nativeOptions(t, browserconfig.Config{Mode: "live", LiveEndpoint: endpoint}, driver)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			connection, err := OpenNative(ctx, t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			value, err := connection.Eval(ctx, "document.title")
			if err != nil || value != `"Example"` {
				t.Fatalf("%s %v", value, err)
			}
			if connection.Mode() != ModeLive {
				t.Fatal("mode changed")
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := connection.Eval(ctx, "effect()"); err == nil {
				t.Fatal("closed browser reopened")
			}
			if _, err := os.Stat(filepath.Join(options.Directory, "browser", "profiles")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("live mode created a profile", err)
			}
		})
	}
}

func TestNativeLiveUnavailableDoesNotLaunch(t *testing.T) {
	options := nativeOptions(t, browserconfig.Config{Mode: "live", LiveProfile: filepath.Join(t.TempDir(), "absent"), Executable: "/unexpected/launcher"}, DriverRod)
	if _, err := OpenNative(t.Context(), t.Context(), options); err == nil {
		t.Fatal("unavailable live selection succeeded")
	}
	if entries, err := os.ReadDir(options.Directory); err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestNativeEndpointRejectsRedirectOrChangedAuthority(t *testing.T) {
	for _, kind := range []string{"redirect", "off-loopback", "other-port", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "redirect":
					http.Redirect(w, r, "http://attacker.example", http.StatusTemporaryRedirect)
				case "off-loopback":
					_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://192.0.2.1:9222/devtools/browser/id"}`))
				case "other-port":
					_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:1/devtools/browser/id"}`))
				case "oversize":
					_, _ = w.Write([]byte(strings.Repeat(" ", 8193)))
				}
			}))
			defer server.Close()
			if _, err := resolveNativeEndpoint(t.Context(), browserconfig.Config{Mode: "live", LiveEndpoint: server.URL}); err == nil {
				t.Fatal("discovery escaped exact configured authority")
			}
		})
	}
}

func TestNativeProfileEndpointIsBounded(t *testing.T) {
	profile := t.TempDir()
	for _, value := range []string{"9222\n/devtools/browser/test\n", "9222\n@attacker.example/devtools/browser/test\n", "0\n/devtools/browser/test\n", strings.Repeat("x", 4097)} {
		if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		endpoint, err := nativeProfileEndpoint(profile)
		if strings.HasPrefix(value, "9222\n/devtools") {
			if err != nil || endpoint != "ws://127.0.0.1:9222/devtools/browser/test" {
				t.Fatal(endpoint, err)
			}
		} else if err == nil {
			t.Fatal("accepted invalid profile endpoint")
		}
	}
}

func TestNativeHeadlessOwnedProcess(t *testing.T) {
	executable := os.Getenv("WHIP_BROWSER_NATIVE_TEST_BINARY")
	if executable == "" {
		t.Skip("owned Chromium fixture binary unavailable")
	}
	for _, driver := range []string{DriverRod, DriverChromedp} {
		t.Run(driver, func(t *testing.T) {
			options := nativeOptions(t, browserconfig.Config{Mode: "headless", Executable: executable}, driver)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			connection, err := OpenNative(ctx, t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			value, err := connection.Eval(ctx, "2+3")
			if err != nil || value != "5" {
				t.Fatal(value, err)
			}
			screenshot, err := connection.Screenshot(ctx, 256)
			if err != nil || len(screenshot) == 0 {
				t.Fatal(len(screenshot), err)
			}
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-connection.processDone:
			default:
				t.Fatal("owned Chrome was not joined")
			}
			if _, err := os.Stat(filepath.Join(options.Directory, "browser", "profiles", options.Profile)); err != nil {
				t.Fatal("retained profile missing", err)
			}
		})
	}
}

func TestNativeExtensionOwnsPrivateRelay(t *testing.T) {
	for _, driver := range []string{DriverRod, DriverChromedp} {
		t.Run(driver, func(t *testing.T) {
			options := nativeOptions(t, browserconfig.Config{Mode: "extension"}, driver)
			directory := filepath.Join(options.Directory, "browser", "extension")
			if _, err := extrelay.WriteExtension(directory); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			type opened struct {
				connection *NativeConnection
				err        error
			}
			result := make(chan opened, 1)
			go func() { connection, err := OpenNative(ctx, t.Context(), options); result <- opened{connection, err} }()
			var state struct{ Addr, Token string }
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for state.Token == "" {
				raw, err := os.ReadFile(filepath.Join(directory, "relay.json"))
				if err == nil {
					_ = json.Unmarshal(raw, &state)
				}
				if state.Token != "" {
					break
				}
				select {
				case failure := <-result:
					t.Fatal(failure.err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			nc, reader, _, err := ws.Dial(ctx, "ws://"+state.Addr+"/ext?token="+state.Token)
			if err != nil {
				cancel()
				<-result
				t.Fatal(err)
			}
			if reader == nil {
				reader = bufio.NewReader(nc)
			}
			extension := &fakeExtClient{nc: nc, br: reader}
			extension.send(t, `{"method":"whip.attached","params":{"tabId":7,"title":"Example","url":"https://example.com/"}}`)
			var group sync.WaitGroup
			group.Go(func() { extension.answerLoop(t) })
			defer func() { extension.close(); group.Wait() }()
			var live opened
			select {
			case live = <-result:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if live.err != nil {
				t.Fatal(live.err)
			}
			defer live.connection.Close()
			value, err := live.connection.Eval(ctx, "document.title")
			if err != nil || value != `"Example"` {
				t.Fatal(value, err)
			}
			if err := live.connection.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(directory, "relay.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("relay credentials survived Close", err)
			}
		})
	}
}
