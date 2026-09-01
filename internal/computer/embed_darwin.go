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
	return filepath.Join(home, ".whip", "bin", "whip-computer"), nil
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
	if existing, err := os.ReadFile(dest); err == nil && bytesEqual(existing, helperBinary) { //nolint:gosec // dest is fixed under ~/.whip/bin
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, helperBinary, 0o700); err != nil { //nolint:gosec // the embedded helper must remain executable
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
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
