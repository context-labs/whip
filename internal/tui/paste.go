package tui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// readClipboardImage returns image bytes and their format extension from the
// system clipboard, or ("", nil, nil) when the clipboard holds no image.
// Tries the built-in macOS pasteboard, wl-paste (Wayland), xclip then xsel
// (X11), pngpaste (a macOS fallback), and PowerShell (Windows/WSL).
func readClipboardImage() (string, []byte, error) {
	ext, data, err := macOSPasteImage()
	if err != nil || data != nil {
		return ext, data, err
	}

	for _, tool := range []struct {
		name string
		fn   func() (string, []byte, error)
	}{
		{"wl-paste", wlPasteImage},
		{"xclip", xclipImage},
		{"xsel", xselImage},
		{"pngpaste", pngpasteImage},
		{"powershell.exe", powershellImage},
	} {
		if _, err := exec.LookPath(tool.name); err != nil {
			continue
		}
		ext, data, err := tool.fn()
		if err != nil || data != nil {
			return ext, data, err
		}
	}
	return "", nil, nil
}

// macOSPasteImage uses AppKit through the built-in osascript command, so image
// paste works on a stock Mac rather than requiring the third-party pngpaste.
// It requests PNG directly, then converts any image AppKit can read to PNG.
const macOSPasteImageScript = `ObjC.import('AppKit')

function run(argv) {
  const pasteboard = $.NSPasteboard.generalPasteboard
  let data = pasteboard.dataForType($.NSPasteboardTypePNG)
  if (!data) {
    const image = $.NSImage.alloc.initWithPasteboard(pasteboard)
    if (image) {
      const rep = $.NSBitmapImageRep.imageRepWithData(image.TIFFRepresentation)
      if (rep) {
        data = rep.representationUsingTypeProperties(
          $.NSBitmapImageFileTypePNG,
          $.NSDictionary.dictionary,
        )
      }
    }
  }
  if (data) data.writeToFileAtomically($(argv[0]), true)
}`

func macOSPasteImage() (string, []byte, error) {
	if runtime.GOOS != "darwin" {
		return "", nil, nil
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		return "", nil, nil
	}

	tmp, err := os.CreateTemp("", "whip-paste-*.png")
	if err != nil {
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		return "", nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := run("osascript", "-l", "JavaScript", "-e", macOSPasteImageScript, tmp.Name()); err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, nil
	}
	return "png", data, nil
}

// hasImageType reports whether types contains an image MIME type.
func hasImageType(types []byte) (string, bool) {
	for line := range strings.SplitSeq(string(types), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "image/"); ok {
			return after, true // keep the subtype as the file extension
		}
	}
	return "", false
}

func run(name string, args ...string) ([]byte, error) {
	// Clipboard tools can hang when no selection owner answers; bound them.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func wlPasteImage() (string, []byte, error) {
	types, err := run("wl-paste", "--list-types")
	if err != nil {
		return "", nil, err
	}
	ext, ok := hasImageType(types)
	if !ok {
		return "", nil, nil
	}
	data, err := run("wl-paste", "--type", "image/"+ext)
	return ext, data, err
}

func xclipImage() (string, []byte, error) {
	targets, err := run("xclip", "-selection", "clipboard", "-o", "-t", "TARGETS")
	if err != nil {
		return "", nil, err
	}
	ext, ok := hasImageType(targets)
	if !ok {
		return "", nil, nil
	}
	data, err := run("xclip", "-selection", "clipboard", "-o", "-t", "image/"+ext)
	return ext, data, err
}

func xselImage() (string, []byte, error) {
	// xsel has no TARGETS listing; probe for image output directly.
	data, err := run("xsel", "--clipboard", "--output", "--target", "image/png")
	if err != nil || len(data) == 0 {
		return "", nil, err
	}
	return "png", data, nil
}

func pngpasteImage() (string, []byte, error) {
	tmp, err := os.CreateTemp("", "whip-paste-*.png")
	if err != nil {
		return "", nil, err
	}
	_ = tmp.Close() // pngpaste writes the path itself; the close error is not actionable
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := exec.CommandContext(context.Background(), "pngpaste", tmp.Name()).Run(); err != nil {
		return "", nil, nil // no image on the clipboard
	}
	data, err := os.ReadFile(tmp.Name())
	if len(data) == 0 {
		return "", nil, err
	}
	return "png", data, err
}

func powershellImage() (string, []byte, error) {
	const script = `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$img = [Windows.Forms.Clipboard]::GetImage(); ` +
		`if ($img -eq $null) { exit 1 }; ` +
		`$img.Save([Console]::OpenStandardOutput(), [System.Drawing.Imaging.ImageFormat]::Png)`
	data, err := exec.CommandContext(context.Background(), "powershell.exe", "-NoProfile", "-Command", script).Output()
	if err != nil || len(data) == 0 {
		return "", nil, err
	}
	return "png", data, nil
}

// pastedImagePath recognizes a single pasted local image path, including the
// extension-less temporary paths emitted by macOS screenshot previews. A
// Finder drag pastes a backslash-escaped path ("a\ b.png"); unescape it
// before statting.
func pastedImagePath(text string) (string, bool) {
	path := strings.TrimSpace(text)
	if u, err := url.Parse(path); err == nil && u.Scheme == "file" {
		if u.Host != "" && u.Host != "localhost" {
			return "", false
		}
		path = u.Path
	}
	path = unescapePath(path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	if imageFileExtension(path, nil) != "" {
		return path, true
	}

	f, err := os.Open(path) //nolint:gosec // G304: path comes directly from the user's paste
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	header := make([]byte, 12)
	n, err := f.Read(header)
	if err != nil && n == 0 {
		return "", false
	}
	return path, imageFileExtension("", header[:n]) != ""
}

// imageFileExtension returns a supported image extension from a filename or
// magic bytes. The latter keeps extension-less screenshot previews attachable.
func imageFileExtension(path string, data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(data) >= 3 && bytes.Equal(data[:3], []byte("\xff\xd8\xff")):
		return "jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	case bytes.HasPrefix(data, []byte("BM")):
		return "bmp"
	}

	ext := strings.ToLower(filepath.Ext(path))
	if imageExtsForMention[ext] {
		return strings.TrimPrefix(ext, ".")
	}
	return ""
}

// pasteImageFileCmd copies a path pasted by the terminal into whip's paste
// directory. Screenshot preview files are temporary — macOS keeps the
// bottom-right hover thumbnail in a transient staging dir that can be swept
// away before the next user turn reads the @mention — so the bytes are copied
// off the source path immediately. The original display name travels with the
// copy so the chip can keep showing the human-readable filename.
func pasteImageFileCmd(path string) tea.Msg {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path was validated from the user's paste
	if err != nil {
		return imageMsg{err: err}
	}
	ext := imageFileExtension(path, data)
	if ext == "" {
		return imageMsg{err: errors.New("pasted file is not a supported image")}
	}
	display := filepath.Base(path)
	path, err = saveClipboardImage(ext, data)
	if err != nil {
		return imageMsg{err: err}
	}
	return imageMsg{path: path, display: display}
}

// saveClipboardImage writes data to ~/.whip/pastes/ and returns the path.
// Images are normalized first (bounded dims, byte budget) so a HiDPI
// screenshot doesn't ride the context at full pixel cost for the rest of the
// session.
func saveClipboardImage(ext string, data []byte) (string, error) {
	ext, data = llm.NormalizeImage(ext, data)
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "pastes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b := make([]byte, 3)
	rand.Read(b)
	name := fmt.Sprintf("%s-%s.%s", time.Now().Format("20060102-150405"), hex.EncodeToString(b), ext)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if err := root.WriteFile(name, data, 0o600); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// pasteImageCmd reads the clipboard image off the UI thread.
func pasteImageCmd() tea.Msg {
	ext, data, err := readClipboardImage()
	if err != nil {
		return imageMsg{err: err}
	}
	if data == nil {
		return imageMsg{}
	}
	path, err := saveClipboardImage(ext, data)
	if err != nil {
		return imageMsg{err: err}
	}
	// A clipboard paste is anonymous — no file on disk — so the chip carries
	// no display name.
	return imageMsg{path: path}
}

// pastedImage records an image the terminal pasted this session. The chip
// shown in the input and transcript references it by its session number n,
// which stays stable across turns so [Image 1], [Image 2], … read back to the
// right on-disk copy at submit.
type pastedImage struct {
	n       int    // 1-based session image number
	path    string // stable on-disk copy (always readable at submit)
	display string // original basename for the chip; "" for an anonymous clipboard paste
}

// chipSentinel is an invisible zero-width space inserted before the closing
// bracket of every paste-inserted chip. The user can't type it, so the
// expandImageChips regex requires it — pattern-matching only real chips, not
// hand-typed "[Image 1]" text that would otherwise attach images[0].
const chipSentinel = "\u200b"

// chipText renders the compact chip for the image, e.g. "[Image 1]" for an
// anonymous paste or "[Image 1: Screenshot 2026-09-04…png]" when a source
// filename is known. The number is what maps back to the stored copy at
// submit, so the display name is truncated aggressively without losing the
// identity. A literal ] in the filename would close the chip early for the
// expandImageChips regex and silently drop the attachment, so brackets are
// stripped from the display snippet (cosmetic only — resolution uses n).
// The invisible chipSentinel before ] marks the chip as paste-inserted.
func (p pastedImage) chipText() string {
	if p.display == "" {
		return fmt.Sprintf("[Image %d%s]", p.n, chipSentinel)
	}
	display := strings.NewReplacer("[", "(", "]", ")").Replace(p.display)
	return fmt.Sprintf("[Image %d: %s%s]", p.n, truncateImageName(display, maxImageNameRunes), chipSentinel)
}

// maxImageNameRunes is the widest a chip filename snippet may be before the
// ellipsis truncation kicks in.
const maxImageNameRunes = 24

// truncateImageName shortens a display name for a chip to at most max runes of
// display width, keeping the file extension and as much of the stem as fits,
// joined by "…" so a long screenshot name collapses readably.
func truncateImageName(name string, limit int) string {
	if runewidth.StringWidth(name) <= limit {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	const ellipsis = "…"
	budget := limit - runewidth.StringWidth(ellipsis) - runewidth.StringWidth(ext)
	if budget < 1 {
		// No room for any stem + "…" + ext; keep just the extension if it fits.
		if runewidth.StringWidth(ext) <= limit {
			return ext
		}
		return ellipsis
	}
	// Trim the stem by display width, advancing rune by rune.
	var cut int
	width := 0
	for _, r := range stem {
		w := runewidth.RuneWidth(r)
		if width+w > budget {
			break
		}
		width += w
		cut += len(string(r))
	}
	return stem[:cut] + ellipsis + ext
}

// expandImageChips rewrites [Image N …] chip tokens in the input text back into
// "@<path>" mentions so the normal @image attachment machinery (imageParts +
// expandMentions) picks them up at submit. The input box itself keeps showing
// the chip; only the prepared text sent to the model reverts to the real path.
func (m *model) expandImageChips(text string) string {
	return imageChipRe.ReplaceAllStringFunc(text, func(tok string) string {
		sm := imageChipRe.FindStringSubmatch(tok)
		if len(sm) < 2 {
			return tok
		}
		n, err := strconv.Atoi(sm[1])
		if err != nil || n < 1 || n > len(m.images) {
			return tok // unrecognized chip: keep it literal rather than guessing
		}
		return "@" + m.images[n-1].path
	})
}

// imageChipRe matches the [Image N] / [Image N: name] chips inserted by paste.
// Only the leading number matters for resolving back to the stored copy; the
// display-name part (opaque to the model) is ignored. The trailing zero-width
// sentinel (chipSentinel) is required, so hand-typed "[Image 1]" text — which
// lacks it — never matches and never attaches an image the user didn't paste.
var imageChipRe = regexp.MustCompile(`\[Image\s+(\d+)(?::[^\]\x{200b}]*)?\x{200b}\]`)
