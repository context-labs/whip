package browserhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/browser"
)

func TestScopedRodBackendUsesOnlyAuthorizedPeerAndPublishesImage(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	var encoded bytes.Buffer
	if e := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); e != nil {
		t.Fatal(e)
	}
	data := encoded.Bytes()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var methods []string
	var mu sync.Mutex
	served := make(chan error, 1)
	go func() {
		for {
			n, e := p.Next(ctx)
			if e != nil {
				if ctx.Err() != nil {
					served <- nil
				} else {
					served <- e
				}
				return
			}
			if n.Command == nil {
				continue
			}
			command := n.Command
			r := response(command)
			if command.Kind == "cdp" {
				var args struct {
					Method string `json:"method"`
				}
				if e = json.Unmarshal(command.Arguments, &args); e != nil {
					served <- e
					return
				}
				mu.Lock()
				methods = append(methods, args.Method)
				mu.Unlock()
				switch args.Method {
				case "Page.getFrameTree":
					r.Result = json.RawMessage(`{"frameTree":{"frame":{"id":"frame","url":"about:blank","securityOrigin":"null","mimeType":"text/html"}}}`)
				case "Runtime.evaluate":
					r.Result = json.RawMessage(`{"result":{"type":"string","value":"{\"url\":\"about:blank\",\"title\":\"page\",\"w\":2,\"h\":2}"}}`)
				case "Page.getLayoutMetrics":
					r.Result = json.RawMessage(`{"cssLayoutViewport":{"clientWidth":2,"clientHeight":2}}`)
				case "Page.captureScreenshot":
					if e = p.UploadScreenshot(command.CommandID, "root", command.Scope.ProviderEpoch, command.Scope.AttachmentGeneration, 0, data); e != nil {
						served <- e
						return
					}
					sum := sha256.Sum256(data)
					r.Screenshot = &Screenshot{Size: len(data), MediaType: "image/jpeg", Digest: hex.EncodeToString(sum[:])}
				}
			}
			if e = p.Settle(r); e != nil {
				served <- e
				return
			}
		}
	}()
	published := 0
	_, e := l.Run(ctx, "rod-batch", allow, func(ctx context.Context, b *Batch) error {
		client, err := b.NewCDPClient()
		if err != nil {
			return err
		}
		defer client.Close()
		backend, err := browser.NewDesktopBackend(ctx, client, a.Scope.TabID, func() error { client.Close(); return nil })
		if err != nil {
			return err
		}
		defer func() { _ = backend.Close() }()
		program, err := browser.CompileProgram(`type("café"); screenshot()`)
		if err != nil {
			return err
		}
		_, err = program.Run(ctx, backend, browser.ProgramLimits{Images: 1, ImageBytes: 4 << 20}, func(_ context.Context, shot []byte) error {
			_, decodeErr := jpeg.Decode(bytes.NewReader(shot))
			if decodeErr == nil {
				published++
			}
			return decodeErr
		})
		return err
	})
	cancel()
	if serveErr := <-served; serveErr != nil {
		t.Fatal(serveErr)
	}
	if e != nil {
		t.Fatal(e)
	}
	if published != 1 {
		t.Fatalf("published %d images", published)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) == 0 {
		t.Fatal("scoped backend bypassed peer")
	}
	for _, method := range methods {
		if method == "Browser.close" || method == "Target.createTarget" {
			t.Fatalf("human resource mutation: %s", method)
		}
	}
}

func TestCDPClientCloseCancelsAndJoinsPendingCall(t *testing.T) {
	h, p, _ := fixture(t)
	a := attach(t, h, p, root, "human-tab")
	l := runLease(t, h, root, a.Scope.AttachmentID)
	done := make(chan error, 1)
	clientReady := make(chan *CDPClient, 1)
	callDone := make(chan error, 1)
	go func() {
		_, e := l.Run(context.Background(), "client-close", allow, func(ctx context.Context, b *Batch) error {
			c, err := b.NewCDPClient()
			if err != nil {
				return err
			}
			clientReady <- c
			_, err = c.Call(ctx, "", "Runtime.evaluate", map[string]any{})
			callDone <- err
			return err
		})
		l.Close()
		done <- e
	}()
	begin := next(t, p).Command
	if e := p.Settle(response(begin)); e != nil {
		t.Fatal(e)
	}
	client := <-clientReady
	command := next(t, p).Command
	client.Close()
	if e := <-callDone; !errors.Is(e, ErrUnknown) {
		t.Fatalf("pending effect: %v", e)
	}
	if e := <-done; !errors.Is(e, ErrUnknown) {
		t.Fatalf("batch effect: %v", e)
	}
	if e := p.Settle(response(command)); !errors.Is(e, ErrStale) {
		t.Fatalf("late call: %v", e)
	}
}
