//go:build darwin

// embed_darwin.go — extract the embedded whip-computer helper to a stable
// path (~/.whip/bin/whip-computer) on first use (plan §"Why embed": stable
// path + stable signature = sticky TCC). If no helper is embedded (fresh
// clone before `task driver`), fall back to the driver build tree for dev;
// otherwise computer-use's native tier is unavailable and callers keep the
// osascript tier.

package computer

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
)

const helperFilename = "whip-computer"

// helperBinary is empty until `task driver` builds the Swift driver and
// copies it into internal/computer/bin/ (go:embed needs the file at build
// time; a zero-byte placeholder keeps the build green before then).
//
//go:embed bin/whip-computer
var helperBinary []byte

// helperDest is the stable extraction path TCC binds to.
func helperDest() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".whip", "bin", helperFilename), nil
}

// ensureHelperBinary extracts the embedded helper to ~/.whip/bin (once —
// skipped when the on-disk file already matches the embedded bytes). With an
// empty embed (placeholder), prefer the dev build tree.
func ensureHelperBinary() (string, error) {
	dest, err := helperDest()
	if err != nil {
		return "", err
	}
	if len(helperBinary) == 0 {
		// Dev fallback: the Swift build products in the repo.
		for _, p := range []string{
			"driver/.build/release/whip-computer",
			"driver/.build/debug/whip-computer",
		} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				abs, _ := filepath.Abs(p)
				return abs, nil
			}
		}
		return "", errors.New("no whip-computer helper embedded and none built — run `task driver` (macOS, needs Xcode CLT)")
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if existing, err := root.ReadFile(helperFilename); err == nil && bytesEqual(existing, helperBinary) {
		return dest, nil
	}
	tmp := helperFilename + ".tmp"
	if err := root.WriteFile(tmp, helperBinary, 0o600); err != nil {
		return "", err
	}
	if err := root.Chmod(tmp, 0o700); err != nil {
		_ = root.Remove(tmp)
		return "", err
	}
	if err := root.Rename(tmp, helperFilename); err != nil {
		_ = root.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
