package hostview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

// CreateDirectory creates one human-requested child in an existing host folder.
// It grants no agent access, creates no ancestors and never adopts an existing
// entry. The open parent anchors the mutation if its path is renamed meanwhile.
func CreateDirectory(ctx context.Context, parent, name string) (string, error) {
	if len(parent) > 4096 || !utf8.ValidString(parent) || strings.ContainsRune(parent, 0) || !filepath.IsAbs(parent) {
		return "", invalid("parent must be an absolute directory path of at most 4096 bytes")
	}
	if strings.TrimSpace(name) == "" || len(name) > 255 || !utf8.ValidString(name) || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || !filepath.IsLocal(name) {
		return "", invalid("folder name must be one nonblank component of at most 255 bytes")
	}
	if runtime.GOOS == "windows" && (strings.ContainsAny(name, `<>:"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.ContainsFunc(name, func(r rune) bool { return r < 32 })) {
		return "", invalid("folder name contains characters or aliases that are invalid on Windows")
	}
	path := filepath.Join(parent, name)
	if len(path) > 4096 {
		return "", invalid("created directory path must be at most 4096 bytes")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "", fmt.Errorf("open parent folder: %w", err)
	}
	defer func() { _ = root.Close() }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := root.Mkdir(name, 0o700); err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}
	return path, nil
}
