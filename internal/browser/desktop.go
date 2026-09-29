package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"golang.org/x/image/draw"
)

type DesktopError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e *DesktopError) Error() string { return e.Kind + ": " + e.Message }

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
		Browser:  &Browser{mode: Mode("desktop"), browser: rb, page: page},
		targetID: targetID, closeTransport: closeTransport,
	}, nil
}

type desktopBackend struct {
	*Browser
	targetID       string
	closeTransport func() error
	closeOnce      sync.Once
	closeErr       error
	media          desktopMedia
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

// Screenshot enforces physical JPEG pixel bounds after capture. CSS viewport
// sizes and page-provided devicePixelRatio are not trustworthy pixel limits.
func (b *desktopBackend) Screenshot(ctx context.Context, maxDim int) ([]byte, error) {
	return b.media.capture(ctx, maxDim, b.Browser.Screenshot)
}

type desktopMedia struct {
	mu              sync.Mutex
	screenshots     int
	screenshotBytes int
}

func (b *desktopMedia) capture(ctx context.Context, maxDim int, capture func(context.Context, int) ([]byte, error)) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.screenshots >= 8 || b.screenshotBytes >= 16<<20 {
		return nil, &DesktopError{Kind: "media_limit", Message: "browser batch screenshot limit exceeded"}
	}
	b.screenshots++
	if maxDim <= 0 || maxDim > 2048 {
		maxDim = 2048
	}
	data, err := capture(ctx, maxDim)
	if err != nil {
		return nil, err
	}
	data, err = BoundJPEG(ctx, data, maxDim)
	if err != nil {
		return nil, err
	}
	if b.screenshotBytes+len(data) > 16<<20 {
		return nil, &DesktopError{Kind: "media_limit", Message: "browser batch screenshot bytes exceeded"}
	}
	b.screenshotBytes += len(data)
	return data, nil
}

// BoundJPEG validates and normalizes a bounded JPEG from a browser or native helper.
func BoundJPEG(ctx context.Context, data []byte, maxDim int) ([]byte, error) {
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
