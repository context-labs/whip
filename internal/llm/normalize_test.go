package llm

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"testing"
)

// bigNoisyPNG builds a photo-like PNG (per-pixel noise) so the encoded size
// is large even at moderate dims — solid-color fixtures compress to nothing
// and never exercise the quality ladder.
func bigNoisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestNormalizeImageOversizedShrinksToBudget(t *testing.T) {
	// 2600×2000 noisy PNG — over the 2000px cap, so it must be scaled+re-encoded.
	data := bigNoisyPNG(t, 2600, 2000)
	ext, out := NormalizeImage("png", data)
	if ext != "jpg" {
		t.Fatalf("oversized image should re-encode as jpg, got %q", ext)
	}
	w, h, ok := DecodeImageSize(out)
	if !ok {
		t.Fatal("output must decode")
	}
	if w > NormalizeMaxDim || h > NormalizeMaxDim {
		t.Fatalf("dims must fit the cap, got %d×%d", w, h)
	}
	if len(out) > NormalizeMaxBytes {
		t.Fatalf("output must fit the byte budget, got %d", len(out))
	}
	// aspect preserved: 2600×2000 → 2000×1538
	if w != 2000 || h < 1500 || h > 1545 {
		t.Fatalf("aspect should be preserved, got %d×%d", w, h)
	}
}

func TestNormalizeImageSmallPassthrough(t *testing.T) {
	// A small image already within both budgets passes through untouched —
	// no decode, no re-encode, original bytes and ext.
	data := pngFixture(t, 800, 600)
	ext, out := NormalizeImage("png", data)
	if ext != "png" {
		t.Fatalf("small image should keep its encoding, got %q", ext)
	}
	if !bytes.Equal(out, data) {
		t.Fatal("small image should pass through byte-identical")
	}
}

func TestNormalizeImageCorruptPassthrough(t *testing.T) {
	ext, out := NormalizeImage("png", []byte("definitely not an image"))
	if ext != "png" || !bytes.Equal(out, []byte("definitely not an image")) {
		t.Fatal("undecodable input must pass through unchanged")
	}
}

func TestNormalizeImageNoiseFitsBudget(t *testing.T) {
	// Worst case: max-dim pure noise. The quality ladder must still land ≤5MiB.
	data := bigNoisyPNG(t, 2000, 2000)
	ext, out := NormalizeImage("png", data)
	if len(out) > NormalizeMaxBytes {
		t.Fatalf("even max-dim noise must fit the budget, got %d bytes (%s)", len(out), ext)
	}
}

// The passthrough decision reads only the header: a tiny file whose header
// claims a huge canvas (a decompression-bomb shape) must not be pixel-decoded
// when it is over the dim cap either — it goes to the re-encode path only if
// it actually decodes. Here a header-only truncated PNG within the caps passes
// through without ever needing pixels.
func TestNormalizeImageHeaderOnlyPassthrough(t *testing.T) {
	full := pngFixture(t, 800, 600)
	head := full[:64] // IHDR is complete; pixel data is gone
	if w, h, ok := DecodeImageSize(head); !ok || w != 800 || h != 600 {
		t.Fatalf("fixture: header should still decode, got %d×%d ok=%v", w, h, ok)
	}
	ext, out := NormalizeImage("png", head)
	if ext != "png" || !bytes.Equal(out, head) {
		t.Fatal("an in-budget image must pass through on its header alone (no pixel decode)")
	}
}
