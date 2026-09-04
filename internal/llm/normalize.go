package llm

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"

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
	// NormalizeMaxPixels bounds what NormalizeImage will pixel-decode at all:
	// 64 megapixels (~256MiB RGBA) covers any real display capture; a header
	// declaring more is a decompression bomb, not an image to re-encode.
	NormalizeMaxPixels = 64_000_000
	// normalizeMinScale bounds the quality-fallback loop: below this the
	// image would be illegible anyway, so we stop and return what we have.
	normalizeMinScale = 0.05
)

// jpegQualities are tried in order until the encoded output fits
// NormalizeMaxBytes. 80 is visually lossless for screenshots; 40 is the last
// resort for pathological noise-heavy captures.
var jpegQualities = []int{80, 70, 55, 40}

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
	// Header-only check first: the passthrough decision never needs pixels,
	// and a small-file/huge-canvas PNG must not force a full decode on the
	// paste path just to learn it is already within budget.
	w, h, ok := DecodeImageSize(data)
	if !ok {
		return ext, data
	}
	if len(data) <= NormalizeMaxBytes && w <= NormalizeMaxDim && h <= NormalizeMaxDim {
		return ext, data // already cheap enough; keep the original encoding
	}
	if w*h > NormalizeMaxPixels {
		// A header can declare a canvas no real capture has (a 40-byte PNG
		// claiming 100000²) and image.Decode would allocate for all of it.
		// Pass it through untouched: the provider rejects it, whip stays up.
		return ext, data
	}
	src, err := decodeAny(data)
	if err != nil {
		return ext, data
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

// scaleToFit returns src drawn onto an opaque white canvas, scaled down to
// fit inside maxW×maxH (aspect preserved) when it does not already.
func scaleToFit(src image.Image, maxW, maxH int) image.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	nw, nh := w, h
	if w > maxW || h > maxH {
		scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
		nw, nh = max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	}
	// Always redraw, even at the same size: the result is JPEG-encoded, and
	// JPEG has no alpha, so the source must be composited onto opaque white
	// (transparent pixels otherwise encode black). An in-cap image reaches
	// here when it is over the byte budget.
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// JPEG has no alpha: composite onto opaque white first, or transparent
	// pixels (logos, UI exports) come out black after the re-encode.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}
