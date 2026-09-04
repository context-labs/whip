package llm

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"math/rand"
	"testing"
	"time"
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

// A transparent source re-encoded to JPEG (no alpha) must land on white, not
// on the zero-initialized (black) canvas.
func TestNormalizeImageTransparentFlattensToWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2400, 100)) // over the dim cap; fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	ext, out := NormalizeImage("png", buf.Bytes())
	if ext != "jpg" {
		t.Fatalf("oversized image should re-encode, got %q", ext)
	}
	dec, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := dec.At(10, 10).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("transparent pixels should flatten to white, got rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

// Alpha flattens to white on the byte-budget path too: an in-cap canvas that
// is over 5MiB re-encodes without scaling and must still composite onto white.
func TestNormalizeImageTransparentInCapOverBytesFlattensToWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1500, 1500))
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 0
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= NormalizeMaxBytes {
		t.Skipf("fixture compressed to %d bytes, under the budget", buf.Len())
	}
	ext, out := NormalizeImage("png", buf.Bytes())
	if ext != "jpg" {
		t.Fatalf("over-budget image should re-encode, got %q", ext)
	}
	dec, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := dec.At(700, 700).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("fully transparent pixels should flatten to white, got rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

// A tiny file whose header declares an absurd canvas is passed through, never
// pixel-decoded (image.Decode would allocate for the declared size).
func TestNormalizeImageDeclaredBombPassesThrough(t *testing.T) {
	data := pngFixture(t, 8, 8)
	// IHDR: bytes 16..19 width, 20..23 height, CRC over "IHDR"+13 data bytes at 29..32
	binary.BigEndian.PutUint32(data[16:], 100000)
	binary.BigEndian.PutUint32(data[20:], 100000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	if w, h, ok := DecodeImageSize(data); !ok || w != 100000 || h != 100000 {
		t.Fatalf("fixture: header should decode as 100000², got %d×%d ok=%v", w, h, ok)
	}
	done := make(chan struct{})
	go func() { NormalizeImage("png", data); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("NormalizeImage tried to decode the declared canvas")
	}
	ext, out := NormalizeImage("png", data)
	if ext != "png" || !bytes.Equal(out, data) {
		t.Fatal("bomb-shaped header must pass through untouched")
	}
}
