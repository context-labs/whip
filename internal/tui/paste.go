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
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
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
// extension-less temporary paths emitted by macOS screenshot previews.
func pastedImagePath(text string) (string, bool) {
	path := strings.TrimSpace(text)
	if u, err := url.Parse(path); err == nil && u.Scheme == "file" {
		if u.Host != "" && u.Host != "localhost" {
			return "", false
		}
		path = u.Path
	}
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
// directory. Screenshot preview files are temporary, so retaining a copy is
// necessary before the next user turn reads the @mention.
func pasteImageFileCmd(path string) tea.Msg {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path was validated from the user's paste
	if err != nil {
		return imageMsg{err: err}
	}
	ext := imageFileExtension(path, data)
	if ext == "" {
		return imageMsg{err: errors.New("pasted file is not a supported image")}
	}
	path, err = saveClipboardImage(ext, data)
	if err != nil {
		return imageMsg{err: err}
	}
	return imageMsg{path: path}
}

// saveClipboardImage writes data to ~/.whip/pastes/ and returns the path.
func saveClipboardImage(ext string, data []byte) (string, error) {
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
	return imageMsg{path: path}
}
