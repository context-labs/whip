// embed.go embeds the unpacked extension so `whip browser install` can
// materialize it with no network fetch and no repo checkout.
package extrelay

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/buildinfo"
)

//go:embed extension
var extensionFS embed.FS

// ExtensionDir is where `whip browser install` materializes the unpacked
// extension (and relay.json) for the user to load.
func ExtensionDir(home string) string {
	return filepath.Join(buildinfo.Home(home), "browser", "extension")
}

// RelayStatePath is the file the extension reads for the relay address +
// auth token. Written 0600 — the token grants drive-a-tab access.
func RelayStatePath(home string) string {
	return filepath.Join(ExtensionDir(home), "relay.json")
}

// WriteExtension materializes the embedded extension files (manifest.json,
// background.js) into dir. Returns the files written.
func WriteExtension(dir string) ([]string, error) {
	var written []string
	entries, err := extensionFS.ReadDir("extension")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := extensionFS.ReadFile("extension/" + e.Name())
		if err != nil {
			return written, err
		}
		dst := filepath.Join(dir, e.Name())
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return written, err
		}
		written = append(written, dst)
	}
	return written, nil
}

// WriteRelayState records the live relay's address + token for the extension
// to read. 0600: the token is a drive-a-tab credential on the loopback relay.
func WriteRelayState(home, addr, token string) (string, error) {
	p := RelayStatePath(home)
	body := fmt.Sprintf("{\"addr\":%q,\"token\":%q}\n", addr, token)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		return "", err
	}
	return p, nil
}
