package llm

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// Image normalization bounds what a pasted/attached/screenshot image can cost
// before it ever enters the conversation. A 3410×2646 clipboard PNG costs
// ~11.4k tokens on a 28px-patch vision model and is re-billed on every
// request until compaction drops it; capping at 2000×2000 and re-encoding as
// JPEG cuts that ~4× with no legibility loss a model can perceive.
//
// Ported from opencode's image/image.ts policy: fit inside a max-dims box,
// then re-encode at descending JPEG qualities until the base64 payload fits a
// byte budget.

const (
	// NormalizeMaxDim caps width and height at ingest. 2000px keeps full
	// legibility for UI screenshots (text down to ~9pt stays readable) while
	// bounding the token cost to ~5.1k per image.
	NormalizeMaxDim = 2000
	// NormalizeMaxBytes caps the ENCODED image bytes so the base64 data URL
	// stays under ~6.9MB of wire payload (opencode's 5MiB base64 budget
	// translated to pre-encode bytes).
	NormalizeMaxBytes = 5 * 1024 * 1024
	// normalizeMinScale bounds the quality-fallback loop: below this the
	// image would be illegible anyway, so we stop and return what we have.
	normalizeMinScale = 0.05
)

// jpegQualities are tried in order until the encoded output fits
// NormalizeMaxBytes. 80 is visually lossless for screenshots; 40 is the last
// resort for pathological noise-heavy captures.
var jpegQualities = []int{80, 85, 70, 55, 40}

// NormalizeImage re-encodes an attached image within the size budget:
//
//  1. If the raw bytes already fit NormalizeMaxBytes AND both dims fit
//     NormalizeMaxDim, the input passes through untouched (ext preserved).
//  2. Otherwise the image is decoded, Lanczos-scaled to fit NormalizeMaxDim,
//     and re-encoded as JPEG at descending qualities; if it still exceeds the
//     byte budget the image shrinks by ×0.75 per round and the quality ladder
//     retries (up to 32 rounds, mirroring opencode).
//
// Returns the (possibly unchanged) bytes and the extension of the returned
// encoding. On any decode failure the input is returned as-is — a corrupt
// image is the provider's problem to reject, not a reason to block the paste.
func NormalizeImage(ext string, data []byte) (string, []byte) {
	src, err := decodeAny(data)
	if err != nil {
		return ext, data
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if len(data) <= NormalizeMaxBytes && w <= NormalizeMaxDim && h <= NormalizeMaxDim {
		return ext, data // already cheap enough; keep the original encoding
	}

	img := scaleToFit(src, NormalizeMaxDim, NormalizeMaxDim)
	for range 32 {
		for _, q := range jpegQualities {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
				return ext, data
			}
			if buf.Len() <= NormalizeMaxBytes {
				return "jpg", buf.Bytes()
			}
		}
		// Still too big: shrink ×0.75 and retry the quality ladder.
		nw, nh := img.Bounds().Dx()*3/4, img.Bounds().Dy()*3/4
		if nw < int(float64(NormalizeMaxDim)*normalizeMinScale) || nh < 8 || nw < 8 {
			break
		}
		img = scaleToFit(img, nw, nh)
	}
	// Give up gracefully: return the last (smallest) attempt rather than the
	// giant original — an over-budget JPEG is still cheaper than the PNG.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQualities[len(jpegQualities)-1]}); err != nil {
		return ext, data
	}
	return "jpg", buf.Bytes()
}

// decodeAny decodes any image format registered via the blank imports in
// images.go (png, jpeg, gif, webp, bmp).
func decodeAny(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// scaleToFit returns src scaled down to fit inside maxW×maxH, preserving
// aspect. A src already inside the box is returned unchanged (no resample).
func scaleToFit(src image.Image, maxW, maxH int) image.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= maxW && h <= maxH {
		return src
	}
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	nw, nh := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// NormalizeImagePNG is NormalizeImage for callers holding PNG bytes whose
// extension they don't know (browser screenshot sinks).
func NormalizeImagePNG(data []byte) (string, []byte) {
	return NormalizeImage("png", data)
}

// encodePNG is exported for tests that build fixtures.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}
