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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// readClipboardImage returns image bytes and their format extension from the
// system clipboard, or ("", nil, nil) when the clipboard holds no image.
// Tries wl-paste (Wayland), xclip then xsel (X11), pngpaste (macOS), and
// PowerShell (Windows/WSL).
func readClipboardImage() (string, []byte, error) {
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

// imageExts is the set of image extensions an @mention attaches (mirrors the
// daemon's imageExt table in internal/daemon/input.go).
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

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
	path = strings.ReplaceAll(path, `\ `, " ")
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
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	case bytes.HasPrefix(data, []byte("BM")):
		return "bmp"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if imageExts[ext] {
		return strings.TrimPrefix(ext, ".")
	}
	return ""
}

// pasteImageFileCmd copies a path pasted by the terminal into whip's paste
// directory. Screenshot preview files are temporary — macOS keeps the
// bottom-right hover thumbnail in a transient staging dir that can be swept
// away before the next user turn reads the @mention — so the bytes are copied
// off the source path immediately, landing in the input as the same @path
// mention a clipboard paste (pasteImageCmd) produces.
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
	// Bound the image before it hits disk or the daemon's size cap: a HiDPI
	// screenshot is several times the pixels the model will be sent anyway.
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
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, data, 0o600)
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
