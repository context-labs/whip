package extrelay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
)

var nativeStateMu sync.Mutex

// WriteNativeState publishes into an explicitly installed native extension
// directory. It never installs files, discovers a home, or reuses a token.
func WriteNativeState(directory, addr, token string) error {
	nativeStateMu.Lock()
	defer nativeStateMu.Unlock()
	if _, err := hex.DecodeString(token); err != nil {
		return errors.New("invalid relay token")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		return errors.New("invalid relay address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || len(token) != 48 {
		return errors.New("invalid relay state")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errors.New("native browser extension is not installed")
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{"manifest.json", "background.js"} {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("native browser extension is not installed")
		}
	}
	// The extension's wire keys are lower-case; keep the private record minimal.
	data, _ := json.Marshal(map[string]string{"addr": addr, "token": token})
	name := ".relay-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := root.Rename(name, "relay.json"); err != nil {
		return err
	}
	return syncNativeDirectory(root)
}

// RemoveNativeState cannot erase a newer owner's publication. A failure leaves
// stale credentials pointing at a closed listener, never a working fallback.
func RemoveNativeState(directory, token string) error {
	nativeStateMu.Lock()
	defer nativeStateMu.Unlock()
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open("relay.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	var value struct {
		Token string `json:"token"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &value) != nil || value.Token != token {
		return nil
	}
	if err := root.Remove("relay.json"); err != nil {
		return err
	}
	return syncNativeDirectory(root)
}

func syncNativeDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
