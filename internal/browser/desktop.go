package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"sync"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"golang.org/x/image/draw"
)

// NewDesktopBackend attaches Rod to an already-authorized, one-target client.
// It never launches a browser, discovers endpoints, changes emulation, or owns
// the native guest. The caller must cancel the transport on grant revocation.
func NewDesktopBackend(ctx context.Context, client rod.CDPClient, targetID string, closeTransport func() error) (Backend, error) {
	if client == nil || targetID == "" || closeTransport == nil {
		return nil, errors.New("desktop browser transport is incomplete")
	}
	rb := rod.New().Context(ctx).Client(client).NoDefaultDevice()
	if err := rb.Connect(); err != nil {
		return nil, err
	}
	page, err := rb.PageFromTarget(proto.TargetTargetID(targetID))
	if err != nil {
		return nil, err
	}
	return &desktopBackend{
		Browser:  &Browser{mode: Mode("desktop"), obtained: ObtainedLive, browser: rb, page: page},
		targetID: targetID, closeTransport: closeTransport,
	}, nil
}

type desktopBackend struct {
	*Browser
	targetID        string
	closeTransport  func() error
	closeOnce       sync.Once
	closeErr        error
	mediaMu         sync.Mutex
	screenshots     int
	screenshotBytes int
}

func (b *desktopBackend) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.closeTransport() })
	return b.closeErr
}

func (b *desktopBackend) UseTab(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if target != b.targetID {
		return &DesktopError{Kind: "permission_denied", Message: "only the attached desktop tab is available"}
	}
	return nil
}

func (b *desktopBackend) UploadFiles(context.Context, string, []string) error {
	return &DesktopError{Kind: "unsupported_operation", Message: "agent file upload requires authorized byte transfer; filesystem paths are not accepted"}
}

// Fill uses trusted text insertion, not legacy ASCII native key codes (which
// are platform-specific and silently lose characters in macOS Electron).
func (b *desktopBackend) Fill(ctx context.Context, selector, text string) error {
	sel, err := json.Marshal(selector)
	if err != nil {
		return err
	}
	focused, err := b.Eval(ctx, fmt.Sprintf(`(()=>{const e=document.querySelector(%s);if(!e)return false;e.focus();if(typeof e.select==='function')e.select();else{const s=window.getSelection(),r=document.createRange();r.selectNodeContents(e);s.removeAllRanges();s.addRange(r)}return true})()`, sel))
	if err != nil {
		return err
	}
	if focused != "true" {
		return fmt.Errorf("fill: element not found: %s", selector)
	}
	if text == "" {
		return b.PressKey(ctx, "Backspace")
	}
	return b.TypeText(ctx, text)
}

func (b *desktopBackend) PressKey(ctx context.Context, key string) error {
	def, named := keyDefs[key]
	if !named && utf8.RuneCountInString(key) != 1 {
		return fmt.Errorf("unknown key %q", key)
	}
	p := b.page.Context(ctx)
	down := proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeKeyDown, Key: key}
	if named {
		down.Code, down.WindowsVirtualKeyCode = def.Code, def.Key
	}
	if err := down.Call(p); err != nil {
		return err
	}
	if !named || key == " " {
		if err := b.TypeText(ctx, key); err != nil {
			return err
		}
	} else if def.Text != "" {
		if err := (proto.InputDispatchKeyEvent{Type: proto.InputDispatchKeyEventTypeChar, Key: key, Code: def.Code, Text: def.Text}).Call(p); err != nil {
			return err
		}
	}
	down.Type = proto.InputDispatchKeyEventTypeKeyUp
	return down.Call(p)
}

// Screenshot enforces physical JPEG pixel bounds after capture. CSS viewport
// sizes and page-provided devicePixelRatio are not trustworthy pixel limits.
func (b *desktopBackend) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	b.mediaMu.Lock()
	defer b.mediaMu.Unlock()
	if b.screenshots >= 8 || b.screenshotBytes >= 16<<20 {
		return nil, &DesktopError{Kind: "media_limit", Message: "browser batch screenshot limit exceeded"}
	}
	b.screenshots++
	if maxDim <= 0 || maxDim > 2048 {
		maxDim = 2048
	}
	data, err := b.Browser.Screenshot(ctx, maxDim)
	if err != nil {
		return nil, err
	}
	data, err = boundDesktopJPEG(ctx, data, maxDim)
	if err != nil {
		return nil, err
	}
	if b.screenshotBytes+len(data) > 16<<20 {
		return nil, &DesktopError{Kind: "media_limit", Message: "browser batch screenshot bytes exceeded"}
	}
	b.screenshotBytes += len(data)
	return data, nil
}

func boundDesktopJPEG(ctx context.Context, data []byte, maxDim int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > 8<<20 || maxDim <= 0 || maxDim > 2048 {
		return nil, errors.New("desktop screenshot exceeds bounds")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("desktop screenshot JPEG: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32<<20 {
		return nil, errors.New("desktop screenshot dimensions exceed bounds")
	}
	largest := max(config.Width, config.Height)
	if largest <= maxDim {
		return data, nil
	}
	source, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	width, height := max(1, config.Width*maxDim/largest), max(1, config.Height*maxDim/largest)
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(target, target.Bounds(), source, source.Bounds(), draw.Src, nil)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, target, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
