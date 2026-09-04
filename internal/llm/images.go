package llm

import (
	"bytes"
	"image"

	// Register the decoders image.DecodeConfig dispatches on. whip builds and
	// sends these formats everywhere (paste.go, mentions, browser
	// screenshots), so the blank imports carry no new dependency.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// ImageTokenFloor is the minimum token count an image part is ever estimated
// at: tiny thumbnails cost the provider's fixed wrapper even at 1×1 px.
const ImageTokenFloor = 85

// imagePatch is the vision encoder's patch edge in pixels for the
// moonshot/qwen-style models whip routes to. Token cost of an image is
// ceil(w/patch)·ceil(h/patch) plus a small fixed wrapper.
const imagePatch = 28

// DecodeImageSize returns the pixel dimensions of an encoded image (png, jpg,
// gif, webp, bmp). It reads only the header — no pixel data is decoded.
func DecodeImageSize(data []byte) (w, h int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// ImageTokens estimates the token cost of one image from its pixel
// dimensions. Unknown dimensions (0×0) fall back to the historical flat
// estimate so callers never undercount an image they failed to measure.
func ImageTokens(w, h int) int {
	if w <= 0 || h <= 0 {
		return 1200
	}
	patchCeil := func(n int) int { return (n + imagePatch - 1) / imagePatch }
	t := patchCeil(w) * patchCeil(h)
	if t < ImageTokenFloor {
		return ImageTokenFloor
	}
	return t
}

// PartTokens estimates the token cost of one content part: image parts use
// the pixel estimate when dimensions were recorded at ingest (ContentPart
// carries them as zero-cost struct fields); text parts use chars/4.
func PartTokens(p ContentPart) int {
	if p.Type == "image_url" {
		if p.W > 0 {
			return ImageTokens(p.W, p.H)
		}
		// Dimensions missing (old session rows, hand-built parts): decode the
		// data URL now rather than falling back blind.
		if w, h, ok := p.DecodeDimensions(); ok {
			return ImageTokens(w, h)
		}
		return ImageTokens(0, 0)
	}
	return (len(p.Text) + 3) / 4
}
