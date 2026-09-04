package tui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// firstContentFG finds the foreground color of the first styled run that
// actually contains visible text (glamour lines start with a margin/color
// prefix whose color is not the content color — e.g. code fences report the
// fence's 235 before the code's 251).
func firstContentFG(l string) int {
	// walk SGR-then-text pairs; return the color of the first text-bearing run
	fg := -1
	i := 0
	for i < len(l) {
		if l[i] == 0x1b {
			end := strings.IndexByte(l[i:], 'm')
			if end < 0 {
				break
			}
			seq := l[i : i+end+1]
			var n int
			if _, err := fmt.Sscanf(seq, "\x1b[38;5;%dm", &n); err == nil {
				fg = n
			}
			i += end + 1
			continue
		}
		// consume a text run up to the next escape
		end := strings.IndexByte(l[i:], 0x1b)
		if end < 0 {
			end = len(l) - i
		}
		// skip whitespace-only runs: chroma styles code-block indentation with
		// the fence/background color (235), which is not the text color
		if strings.Trim(l[i:i+end], " \t") != "" && fg >= 0 {
			return fg
		}
		i += end
	}
	return -1
}

// sgrFG extracts the first "38;5;N" foreground index from a rendered line.
func sgrFG(s string) int {
	for i := 0; i+6 < len(s); i++ {
		if strings.HasPrefix(s[i:], "\x1b[38;5;") {
			var n int
			if _, err := fmt.Sscanf(s[i:], "\x1b[38;5;%dm", &n); err == nil {
				return n
			}
		}
	}
	return -1
}

// ansiLuminance approximates perceived luminance (0-1) of an xterm-256 color
// index: 0-15 standard palette, 16-231 cube, 232-255 grayscale ramp.
func ansiLuminance(n int) float64 {
	if n < 0 {
		return -1
	}
	if n >= 232 {
		return float64(n-232)/23*0.9 + 0.05
	}
	if n >= 16 {
		n -= 16
		r, g, b := float64(n/36)/5, float64((n/6)%6)/5, float64(n%6)/5
		return 0.2126*r + 0.7152*g + 0.0722*b
	}
	std := []float64{0, .25, .2, .35, .15, .3, .4, .55, .4, .7, .65, .8, .5, .7, .8, .95}
	return std[n%16]
}

// TestMarkdownContrastBothThemes proves body text meets a minimum luminance
// gap against the assumed background in BOTH themes — the original bug was
// dark-style 252 (lum ~0.83) on a white bg (1.0): gap 0.17, unreadable.
func TestMarkdownContrastBothThemes(t *testing.T) {
	samples := map[string]string{
		"body":      "plain body text",
		"bold":      "**bold text**",
		"inline":    "has `inline code` here",
		"list":      "- item one\n- item two",
		"codeblock": "```go\nfmt.Println(1)\n```",
	}
	for _, light := range []bool{true, false} {
		SetLightTheme(light)
		bgLum := 0.05 // dark bg assumption
		if light {
			bgLum = 1.0 // white bg
		}
		theme := map[bool]string{true: "light", false: "dark"}[light]
		for name, src := range samples {
			out := renderMarkdown(src, 70)
			for l := range strings.SplitSeq(out, "\n") {
				plain := ansi.Strip(l)
				if strings.TrimSpace(plain) == "" {
					continue
				}
				// skip fence-decoration lines (all spaces/box glyphs, e.g. the
				// code-block fence row whose only color is the fence's 235)
				if strings.Trim(plain, " ░│─╌┃·") == "" {
					continue
				}
				fg := firstContentFG(l)
				if fg < 0 {
					continue // terminal default fg: assumed fine
				}
				gap := ansiLuminance(fg) - bgLum
				if gap < 0 {
					gap = -gap
				}
				need := 0.45
				if name == "inline" || name == "codeblock" {
					need = 0.25 // chip bg dominates readability there
				}
				if gap < need {
					t.Errorf("%s theme %q: fg=%d luminance gap %.2f < %.2f (line %q)", theme, name, fg, gap, need, plain)
				}
				break
			}
		}
	}
	SetLightTheme(false)
}

// TestInlineCodeLightChip proves the light theme's inline code is green text
// on the element surface (stock glamour Light used salmon-on-near-white, ~0.1 gap).
func TestInlineCodeLightChip(t *testing.T) {
	SetLightTheme(true)
	defer SetLightTheme(false)
	out := renderMarkdown("use `config.Save` here", 60)
	elem := currentTheme().Surface.Element
	r, g, b, _ := elem.RGBA()
	chip := fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8) // the light theme's Element surface, same fill as the prompt box
	if !strings.Contains(out, chip) {
		t.Errorf("light inline code should sit on the element chip (%s): %q", chip, out)
	}
	if !strings.Contains(out, "38;2;61;154;87") { // #3d9a57 as glamour v2 emits it
		t.Errorf("light inline code text should be the light code green (#3d9a57): %q", out)
	}
}

// ansiColorToRGB maps an xterm-256 index to an RGB color for the PNG probe.
func ansiColorToRGB(n int, light bool) color.RGBA {
	if n < 0 {
		if light {
			return color.RGBA{40, 40, 40, 255}
		}
		return color.RGBA{220, 220, 220, 255}
	}
	if n >= 232 {
		v := uint8(8 + (n-232)*10)
		return color.RGBA{v, v, v, 255}
	}
	if n >= 16 {
		n -= 16
		lv := [6]uint8{0, 95, 135, 175, 215, 255}
		return color.RGBA{lv[n/36], lv[(n/6)%6], lv[n%6], 255}
	}
	std := [16]color.RGBA{
		{0, 0, 0, 255},
		{128, 0, 0, 255},
		{0, 128, 0, 255},
		{128, 128, 0, 255},
		{0, 0, 128, 255},
		{128, 0, 128, 255},
		{0, 128, 128, 255},
		{192, 192, 192, 255},
		{128, 128, 128, 255},
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{255, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 0, 255, 255},
		{0, 255, 255, 255},
		{255, 255, 255, 255},
	}
	return std[n%16]
}

// TestThemeProbeImages renders the same markdown sample on both themes into
// PNGs (text cells painted with their actual fg color over the theme bg) so
// the kimi vision API can judge legibility. Regenerate with:
//
//	go test ./internal/tui/ -run TestThemeProbeImages
func TestThemeProbeImages(t *testing.T) {
	const cellW, cellH, cols = 8, 14, 72
	sample := "I found the bug and **fixed** it.\n\n1. isolate `HOME` in tests\n2. add a `WHIP_HOME` override\n\n```go\nos.Setenv(\"WHIP_HOME\", dir)\n```\n\n| file | change |\n|---|---|\n| config.go | Dir() override |"
	for _, light := range []bool{false, true} {
		SetLightTheme(light)
		lines := strings.Split(renderMarkdown(sample, cols-4), "\n")
		img := image.NewRGBA(image.Rect(0, 0, cols*cellW, (len(lines)+2)*cellH))
		bg := color.RGBA{30, 30, 34, 255}
		name := "/tmp/whip-theme-dark.png"
		if light {
			bg = color.RGBA{255, 255, 255, 255}
			name = "/tmp/whip-theme-light.png"
		}
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				img.Set(x, y, bg)
			}
		}
		for row, l := range lines {
			fg := ansiColorToRGB(sgrFG(l), light)
			for col, r := range []rune(ansi.Strip(l)) {
				if r == ' ' || col >= cols {
					continue
				}
				x0, y0 := col*cellW, (row+1)*cellH
				for y := y0 + 3; y < y0+cellH-3; y++ {
					for x := x0 + 1; x < x0+cellW-1; x++ {
						img.Set(x, y, fg)
					}
				}
			}
		}
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Logf("wrote %s", name)
	}
	SetLightTheme(false)
}
