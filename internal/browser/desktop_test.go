package browser

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestDesktopScreenshotBoundsPhysicalPixels(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2560, 1600))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	result, err := boundDesktopJPEG(t.Context(), encoded.Bytes(), 640)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(result))
	if err != nil || config.Width != 640 || config.Height != 400 {
		t.Fatalf("unbounded HiDPI image: %+v %v", config, err)
	}
	unchanged, err := boundDesktopJPEG(t.Context(), result, 640)
	if err != nil || !bytes.Equal(result, unchanged) {
		t.Fatal("small screenshot reencoded")
	}
}
func TestDesktopScreenshotRejectsInvalidAndCancelled(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not-jpeg"), make([]byte, (8<<20)+1)} {
		if _, err := boundDesktopJPEG(t.Context(), data, 640); err == nil {
			t.Fatal("accepted invalid screenshot")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := boundDesktopJPEG(ctx, nil, 640); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
func TestDesktopScreenshotBatchCapRejectsBeforeCapture(t *testing.T) {
	for _, backend := range []*desktopBackend{{screenshots: 8}, {screenshotBytes: 16 << 20}} {
		if _, err := backend.Screenshot(t.Context(), 640); err == nil {
			t.Fatal("batch cap admitted screenshot")
		}
	}
}

func TestDesktopBackendNeverChangesTargetOrUploadsPaths(t *testing.T) {
	var closed int
	b := &desktopBackend{targetID: "selected", closeTransport: func() error { closed++; return nil }}
	if err := b.UseTab(t.Context(), "selected"); err != nil {
		t.Fatal(err)
	}
	if err := b.UseTab(t.Context(), "foreign"); err == nil {
		t.Fatal("target switch admitted")
	}
	if err := b.UploadFiles(t.Context(), "input", []string{"/private/file"}); err == nil {
		t.Fatal("local path upload admitted")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil || closed != 1 {
		t.Fatalf("close not idempotent: %d %v", closed, err)
	}
	if _, err := NewDesktopBackend(t.Context(), nil, "selected", func() error { return nil }); err == nil {
		t.Fatal("incomplete transport accepted")
	}
}
