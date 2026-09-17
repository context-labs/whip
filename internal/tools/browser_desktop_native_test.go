//go:build browser_desktop_spike

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/go-rod/rod/lib/cdp"
)

// This exercises the actual helper parser and screenshot sink against the
// selected visible Electron guest. Build tag + private control file are both
// required; a skip is explicitly not native acceptance.
func TestBrowserHelpersDesktopNative(t *testing.T) {
	path := os.Getenv("BROWSER_SPIKE_CONTROL")
	if path == "" {
		t.Skip("native Electron fixture required: set BROWSER_SPIKE_CONTROL")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		WSURL    string `json:"wsURL"`
		TargetID string `json:"targetId"`
		Origin   string `json:"origin"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(fixture.WSURL)
	if err != nil || endpoint.Scheme != "ws" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("fixture must be an authenticated loopback shim")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	ws := &cdp.WebSocket{}
	header := http.Header{"Sec-WebSocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="}}
	if err := ws.Connect(ctx, fixture.WSURL, header); err != nil {
		t.Fatalf("connect native fixture: %s", strings.ReplaceAll(err.Error(), fixture.WSURL, "[private endpoint]"))
	}
	t.Cleanup(func() { _ = ws.Close() })
	backend, err := browser.NewDesktopBackend(ctx, cdp.New().Start(ws), fixture.TargetID, ws.Close)
	if err != nil {
		t.Fatal(err)
	}
	var screenshots [][]byte
	code := fmt.Sprintf(`goto(%q); waitFor("#name", true); js("document.querySelector('#name').focus()"); type("native helpers"); print(js("document.querySelector('#name').value")); print(info()); print(tabs()); screenshot()`, fixture.Origin+"/helpers")
	out, err := runBrowserCode(ctx, backend, code, "desktop-spike", true, func(shots [][]byte) {
		screenshots = append(screenshots, shots...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "native helpers") || !strings.Contains(out, fixture.TargetID) || !strings.Contains(out, "1 screenshot(s)") {
		t.Fatalf("helper result missing expected output/scope/image marker: %s", out)
	}
	if len(screenshots) != 1 {
		t.Fatalf("screenshot sink received %d images", len(screenshots))
	}
	image, err := jpeg.DecodeConfig(bytes.NewReader(screenshots[0]))
	if err != nil || image.Width == 0 || image.Height == 0 {
		t.Fatalf("invalid guest JPEG: %+v, %v", image, err)
	}
	t.Logf("native helper parser + screenshot sink: %dx%d JPEG, %d bytes", image.Width, image.Height, len(screenshots[0]))
	if _, err := runBrowserCode(ctx, backend, `useTab("private-app-target")`, "desktop-spike", true, nil); err == nil {
		t.Fatal("helper useTab escaped selected native guest")
	}
}
