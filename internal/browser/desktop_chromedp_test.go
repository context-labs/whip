package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/cdp"
)

type desktopTestClient struct {
	events chan *cdp.Event
	call   func(context.Context, string, string, any) ([]byte, error)
}

func (c *desktopTestClient) Event() <-chan *cdp.Event { return c.events }
func (c *desktopTestClient) Call(ctx context.Context, session, method string, args any) ([]byte, error) {
	return c.call(ctx, session, method, args)
}

func TestDesktopChromeDPUsesOnlyCapturedClientAndTrustedUnicode(t *testing.T) {
	var methods []string
	var inserted []string
	var screenshot bytes.Buffer
	if err := jpeg.Encode(&screenshot, image.NewRGBA(image.Rect(0, 0, 16, 8)), nil); err != nil {
		t.Fatal(err)
	}
	c := &desktopTestClient{events: make(chan *cdp.Event)}
	c.call = func(_ context.Context, session, method string, args any) ([]byte, error) {
		if session != "attachment" {
			t.Fatal("foreign session", session)
		}
		methods = append(methods, method)
		raw, _ := json.Marshal(args)
		switch method {
		case "Runtime.evaluate":
			if strings.Contains(string(raw), "document.readyState") {
				return []byte(`{"result":{"type":"string","value":"complete"}}`), nil
			}
			return []byte(`{"result":{"type":"boolean","value":true}}`), nil
		case "Input.insertText":
			var p struct{ Text string }
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			inserted = append(inserted, p.Text)
		case "Input.dispatchKeyEvent":
			if strings.Contains(string(raw), "nativeVirtualKeyCode") {
				t.Fatal("platform keycode", string(raw))
			}
		case "Page.navigate":
			return []byte(`{"frameId":"frame"}`), nil
		case "DOM.getBoxModel":
			return []byte(`{"model":{"content":[0]}}`), nil
		case "Page.captureScreenshot":
			return fmt.Appendf(nil, `{"data":%q}`, base64.StdEncoding.EncodeToString(screenshot.Bytes())), nil
		}
		return []byte(`{}`), nil
	}
	closed := 0
	b, err := NewDesktopChromeDPBackend(t.Context(), c, "selected", "attachment", func() error { closed++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if err = b.Fill(t.Context(), "input", "hé🌿"); err != nil {
		t.Fatal(err)
	}
	if err = b.PressKey(t.Context(), "🌿"); err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 || inserted[0] != "hé🌿" || inserted[1] != "🌿" {
		t.Fatal(inserted)
	}
	if err = b.Navigate(t.Context(), "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = b.BoxModel(t.Context(), 1); err == nil {
		t.Fatal("malformed box accepted")
	}
	imageBytes, err := b.Screenshot(t.Context(), 4)
	if err != nil {
		t.Fatal(err)
	}
	dimensions, err := jpeg.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil || dimensions.Width != 4 || dimensions.Height != 2 {
		t.Fatal(dimensions, err)
	}
	for i := 1; i < 8; i++ {
		if _, err = b.Screenshot(t.Context(), 4); err != nil {
			t.Fatal(err)
		}
	}
	before := len(methods)
	if _, err = b.Screenshot(t.Context(), 4); err == nil {
		t.Fatal("unbounded screenshots")
	}
	if err = b.UseTab(t.Context(), "foreign"); err == nil {
		t.Fatal("foreign target accepted")
	}
	if err = b.UploadFiles(t.Context(), "input", []string{"/private"}); err == nil {
		t.Fatal("path upload accepted")
	}
	if len(methods) != before {
		t.Fatal("rejected request sent commands")
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil || closed != 1 {
		t.Fatal(closed, err)
	}
	if err = b.TypeText(t.Context(), "late"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, method := range methods {
		if strings.HasPrefix(method, "Target.") || strings.HasPrefix(method, "Browser.") {
			t.Fatal("new browser ownership", method)
		}
	}
}

func TestDesktopChromeDPCloseJoinsEventsAndCancelsBorrowedCall(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	c := &desktopTestClient{events: make(chan *cdp.Event)}
	c.call = func(ctx context.Context, _, method string, _ any) ([]byte, error) {
		if method == "Input.insertText" {
			close(entered)
			<-ctx.Done()
			close(finished)
			return nil, ctx.Err()
		}
		return []byte(`{}`), nil
	}
	b, err := NewDesktopChromeDPBackend(t.Context(), c, "selected", "attachment", func() error { <-finished; return nil })
	if err != nil {
		t.Fatal(err)
	}
	called := make(chan error, 1)
	go func() { called <- b.TypeText(context.Background(), "blocked") }()
	<-entered
	done := make(chan error, 1)
	go func() { done <- b.Close() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not join")
	}
	if err = <-called; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDesktopChromeDPDrainsAndBoundsDialogEvents(t *testing.T) {
	c := &desktopTestClient{events: make(chan *cdp.Event)}
	c.call = func(context.Context, string, string, any) ([]byte, error) { return []byte(`{}`), nil }
	b, err := NewDesktopChromeDPBackend(t.Context(), c, "selected", "attachment", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	send := func(method string, params json.RawMessage) {
		select {
		case c.events <- &cdp.Event{SessionID: "attachment", Method: method, Params: params}:
		case <-time.After(time.Second):
			t.Fatal("event consumer stalled")
		}
	}
	raw, _ := json.Marshal(map[string]string{"type": "prompt", "message": strings.Repeat("🌿", 4096), "defaultPrompt": "default"})
	send("Page.javascriptDialogOpening", raw)
	send("Runtime.consoleAPICalled", json.RawMessage(`{}`)) // rendezvous after prior event was processed
	info, err := b.Info(t.Context())
	if err != nil || info.Dialog == nil || len(info.Dialog.Message) > 4096 || info.Dialog.DefaultPrompt != "default" {
		t.Fatal(info, err)
	}
	if err = b.HandleDialog(true, "answer"); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		send("Runtime.consoleAPICalled", json.RawMessage(`{}`))
	}
	send("Page.javascriptDialogClosed", json.RawMessage(`{}`))
	send("Runtime.consoleAPICalled", json.RawMessage(`{}`))
	concrete := b.(*desktopChromeDP)
	concrete.dialogMu.Lock()
	pending := concrete.dialog
	concrete.dialogMu.Unlock()
	if pending != nil {
		t.Fatal("closed dialog retained")
	}
	var joined sync.WaitGroup
	for range 4 {
		joined.Go(func() { _ = b.Close() })
	}
	joined.Wait()
}
