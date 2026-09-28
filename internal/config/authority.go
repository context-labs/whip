package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

// ErrRevisionConflict requires a fresh snapshot before another update.
var ErrRevisionConflict = errors.New("host configuration changed; read its current revision")

// All package writers share this lock, including bootstrap Save/Initialize.
// The runtime's exclusive directory ownership supplies the process boundary;
// arbitrary external editors do not participate in compare-and-set updates.
var writeMu sync.Mutex

// Snapshot is a caller-owned declaration and the revision of its exact file
// bytes. Unlike Load, it does not expand omitted resource defaults. No maps or
// pointers are shared with an authority or another snapshot.
type Snapshot struct {
	Host     Host
	Revision string
}

// Authority serializes host-owned updates to the same file read by Load. It
// retains only the explicit directory, never a second configuration copy.
type Authority struct {
	directory string
	// A failed directory sync follows publication: callers must reread before
	// retrying. Kept separate so this boundary can be tested deterministically.
	syncDirectory func(*os.File) error
}

// NewAuthority selects an existing explicit config directory. It performs no
// initialization, credential resolution, or network work.
func NewAuthority(directory string) (*Authority, error) {
	if directory == "" {
		return nil, fmt.Errorf("%w: configuration directory is required", session.ErrInvalid)
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	return &Authority{directory: path, syncDirectory: (*os.File).Sync}, nil
}

func (a *Authority) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	directory, err := openDirectory(a.directory, false)
	if err != nil {
		return Snapshot{}, err
	}
	defer directory.Close()
	value, _, err := readSnapshot(directory)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return value, nil
}

// Update applies a short, side-effect-free patch to fresh disk state. The patch
// must not perform filesystem/network work or call another config writer.
// Expected must come from Snapshot; even an unchanged patch requires a current
// revision. A publication error may follow rename, so reread before retrying.
func (a *Authority) Update(ctx context.Context, expected string, patch func(*Host) error) (Snapshot, error) {
	if expected == "" || patch == nil {
		return Snapshot{}, fmt.Errorf("%w: configuration revision and patch are required", session.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	directory, err := openDirectory(a.directory, false)
	if err != nil {
		return Snapshot{}, err
	}
	defer directory.Close()
	before, original, err := readSnapshot(directory)
	if err != nil {
		return Snapshot{}, err
	}
	if before.Revision != expected {
		return Snapshot{}, ErrRevisionConflict
	}
	unchanged, err := encodeHost(before.Host)
	if err != nil {
		return Snapshot{}, err
	}
	if err := patch(&before.Host); err != nil {
		return Snapshot{}, err
	}
	raw, err := encodeHost(before.Host)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if !bytes.Equal(raw, unchanged) {
		if err := publishHost(ctx, directory, raw, false, a.syncDirectory); err != nil {
			return Snapshot{}, err
		}
		return snapshot(raw)
	}
	// A previous publication may have returned a directory-sync error. Retrying
	// an already visible setup must confirm durability without rewriting it.
	if err := a.syncDirectory(directory); err != nil {
		return Snapshot{}, fmt.Errorf("confirm host configuration durability: %w", err)
	}
	// Decode independently so a patch retaining its argument cannot later alter
	// the returned snapshot. Keep the original exact-bytes revision on no-op.
	return snapshot(original)
}

// EnsureSubscription installs only the fixed ChatGPT subscription route. Valid
// existing model declarations and all defaults remain untouched. An API/custom
// route using this identity is a conflict, never something login may overwrite.
func (h *Host) EnsureSubscription() error {
	if route, ok := h.Providers["openai-codex"]; ok && route.Kind != "openai-codex" {
		return fmt.Errorf("%w: openai-codex is configured as another provider route", ErrRevisionConflict)
	}
	if err := h.Validate(); err != nil {
		return err
	}
	if h.Providers == nil {
		h.Providers = map[string]Provider{}
	}
	if _, ok := h.Providers["openai-codex"]; !ok {
		if len(h.Providers) == 128 {
			return fmt.Errorf("%w: too many provider routes", session.ErrInvalid)
		}
		h.Providers["openai-codex"] = Provider{Kind: "openai-codex"}
	}
	return nil
}

func snapshot(raw []byte) (Snapshot, error) {
	if !utf8.Valid(raw) {
		return Snapshot{}, fmt.Errorf("%w: host configuration must be UTF-8", session.ErrInvalid)
	}
	var host Host
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&host); err != nil {
		return Snapshot{}, fmt.Errorf("decode host configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Snapshot{}, fmt.Errorf("%w: trailing host configuration data", session.ErrInvalid)
	}
	if err := host.Validate(); err != nil {
		return Snapshot{}, err
	}
	digest := sha256.Sum256(raw)
	return Snapshot{Host: host, Revision: hex.EncodeToString(digest[:])}, nil
}

func encodeHost(host Host) ([]byte, error) {
	if err := host.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(host, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > session.MaxDocumentBytes {
		return nil, fmt.Errorf("%w: host configuration exceeds size limit", session.ErrInvalid)
	}
	return append(raw, '\n'), nil
}
