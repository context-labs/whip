package config

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

// ProviderDisconnect keeps declarations so a failed credential cleanup can be
// retried without losing its owner or changing model defaults.
type ProviderDisconnect struct {
	Snapshot        Snapshot
	CredentialState string
	LocalFailure    string
}

// CanDisconnectProvider reports whether Whip manages this credential. Disconnect
// still checks the current revision, shared references and cleanup safety.
func (a *Authority) CanDisconnectProvider(provider Provider) bool {
	_, owned := a.ownedKeyName(provider)
	return owned || provider.Kind == "openai-codex" || provider.CredentialSource == "inference-net"
}

// DisconnectProvider checks the current revision and all shared references
// before clearing a managed credential. Account services call this while holding
// their login lock; clear must cancel old flows and revoke only local credentials
// without calling config writers or doing network work.
func (a *Authority) DisconnectProvider(ctx context.Context, revision, id, accountSource string, clear func() error) (ProviderDisconnect, error) {
	if revision == "" || session.ValidateID(id) != nil {
		return ProviderDisconnect{}, session.ErrInvalid
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ProviderDisconnect{}, err
	}
	directory, err := openDirectory(a.directory, false)
	if err != nil {
		return ProviderDisconnect{}, err
	}
	defer directory.Close()
	before, _, err := readSnapshot(directory)
	if err != nil {
		return ProviderDisconnect{}, err
	}
	if before.Revision != revision {
		return ProviderDisconnect{}, ErrRevisionConflict
	}
	provider, exists := before.Host.Providers[id]
	if !exists {
		return ProviderDisconnect{}, session.ErrInvalid
	}
	source := provider.CredentialSource
	if provider.Kind == "openai-codex" {
		source = "openai-codex"
	}
	if accountSource != "" && (source != accountSource || clear == nil) || accountSource == "" && (source == "openai-codex" || source == "inference-net") {
		return ProviderDisconnect{}, session.ErrInvalid
	}
	result := ProviderDisconnect{Snapshot: before, CredentialState: "preserved_external"}
	managedAccount := accountSource != ""
	name, owned := a.ownedKeyName(provider)
	if !managedAccount && !owned {
		return result, nil
	}
	shared := false
	for otherID, other := range before.Host.Providers {
		if otherID == id {
			continue
		}
		if managedAccount && (source == "openai-codex" && other.Kind == "openai-codex" || source == "inference-net" && other.CredentialSource == source) || !managedAccount && sameCredentialFile(provider.CredentialFile, other.CredentialFile) {
			shared = true
		}
	}
	// A shared source remains usable by other routes. Disable only this route
	// and report that its credential was preserved rather than claiming logout.
	if provider.CredentialEpoch == ^uint64(0) {
		return ProviderDisconnect{}, session.ErrInvalid
	}
	provider.CredentialEpoch++
	provider.Disabled = shared
	before.Host.Providers[id] = provider
	raw, err := encodeHost(before.Host)
	if err != nil {
		return ProviderDisconnect{}, err
	}
	if err := publishHost(ctx, directory, raw, false, a.syncDirectory); err != nil {
		return ProviderDisconnect{}, err
	}
	result.Snapshot, err = snapshot(raw)
	if err != nil {
		return ProviderDisconnect{}, err
	}
	result.CredentialState = "cleared"
	if shared {
		result.CredentialState = "preserved_shared"
	}
	if managedAccount && !shared {
		err = clear()
	} else if !shared {
		err = removeOwnedKey(directory, name)
		if err == nil {
			err = a.syncDirectory(directory)
		}
	}
	if err != nil {
		result.CredentialState = "pending"
		result.LocalFailure = "Credential cleanup is not confirmed; reread provider settings and retry Disconnect"
	}
	return result, nil
}

// PublishKey reserves this namespace inside the private authority directory.
// Explicit external files, symlinks and arbitrary lookalike filenames never
// acquire ownership merely because they appear in a provider declaration.
func (a *Authority) ownedKeyName(provider Provider) (string, bool) {
	if provider.CredentialSource != "file" {
		return "", false
	}
	canonical, err := filepath.EvalSymlinks(a.directory)
	if err != nil || filepath.Dir(provider.CredentialFile) != canonical {
		return "", false
	}
	name := filepath.Base(provider.CredentialFile)
	digest := strings.TrimSuffix(strings.TrimPrefix(name, "provider-key-"), ".txt")
	_, err = hex.DecodeString(digest)
	return name, err == nil && len(digest) == 64 && name == "provider-key-"+digest+".txt" && strings.ToLower(digest) == digest
}

func sameCredentialFile(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	if filepath.Clean(first) == filepath.Clean(second) {
		return true
	}
	a, errA := os.Stat(first)
	b, errB := os.Stat(second)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

func removeOwnedKey(directory *os.File, name string) error {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "provider key")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0o077 != 0 || info.Size() > maxCredentialBytes {
		return ErrKeyStorage
	}
	return unix.Unlinkat(int(directory.Fd()), name, 0)
}
