package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
	"golang.org/x/sys/unix"
)

const maxCredentialBytes = 64 << 10

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CredentialCommand is an administrator-authored executable and argument vector,
// never shell input. Environment lists only names to inherit from the host.
// The command runs in / with no other environment, stdin, or extra descriptors;
// it must finish within ten seconds and emit a single raw key on stdout.
type CredentialCommand struct {
	Executable  string   `json:"executable"`
	Arguments   []string `json:"arguments,omitempty"`
	Environment []string `json:"environment,omitempty"`
}

func (p Provider) credentialSource() string {
	if p.CredentialSource != "" {
		return p.CredentialSource
	}
	if p.CredentialEnv != "" {
		return "env"
	}
	return "none"
}

func (p Provider) validateCredentialSource() error {
	if p.Kind == "openai-codex" {
		if p.CredentialSource != "" || p.CredentialEnv != "" || p.CredentialFile != "" || p.CredentialCommand != nil {
			return fmt.Errorf("%w: subscription routes cannot use API credentials", session.ErrInvalid)
		}
		return nil
	}
	switch p.credentialSource() {
	case "env":
		if len(p.CredentialEnv) > 256 || !environmentName.MatchString(p.CredentialEnv) || p.CredentialFile != "" || p.CredentialCommand != nil {
			return fmt.Errorf("%w: environment credentials require one environment name", session.ErrInvalid)
		}
	case "file":
		if !credentialPath(p.CredentialFile) || p.CredentialEnv != "" || p.CredentialCommand != nil {
			return fmt.Errorf("%w: file credentials require one clean absolute file path", session.ErrInvalid)
		}
	case "command":
		if p.CredentialCommand == nil || p.CredentialEnv != "" || p.CredentialFile != "" {
			return fmt.Errorf("%w: command credentials require one command declaration", session.ErrInvalid)
		}
		return p.CredentialCommand.validate()
	case "none":
		if p.CredentialEnv != "" || p.CredentialFile != "" || p.CredentialCommand != nil {
			return fmt.Errorf("%w: no-auth routes cannot declare credentials", session.ErrInvalid)
		}
	case "inference-net":
		if p.Kind != "openai-chat" || p.BaseURL != "https://api.inference.net/v1" || p.CredentialEnv != "" || p.CredentialFile != "" || p.CredentialCommand != nil {
			return fmt.Errorf("%w: managed Inference.net credentials require their exact chat gateway and no other credential", session.ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported credential source", session.ErrInvalid)
	}
	return nil
}

func credentialPath(path string) bool {
	return len(path) <= 4096 && utf8.ValidString(path) && !strings.ContainsRune(path, 0) &&
		filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) != string(filepath.Separator)
}

func (c CredentialCommand) validate() error {
	if !credentialPath(c.Executable) || len(c.Arguments) > 64 || len(c.Environment) > 64 {
		return fmt.Errorf("%w: credential command exceeds declaration bounds or lacks an absolute executable", session.ErrInvalid)
	}
	size := len(c.Executable)
	for _, argument := range c.Arguments {
		if !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) || len(argument) > 4096 {
			return fmt.Errorf("%w: invalid credential command argument", session.ErrInvalid)
		}
		size += len(argument)
	}
	seen := make(map[string]bool, len(c.Environment))
	for _, name := range c.Environment {
		if len(name) > 256 || !environmentName.MatchString(name) || seen[name] {
			return fmt.Errorf("%w: invalid credential command environment declaration", session.ErrInvalid)
		}
		seen[name] = true
		size += len(name)
	}
	if size > maxCredentialBytes {
		return fmt.Errorf("%w: credential command declaration exceeds size limit", session.ErrInvalid)
	}
	return nil
}

// Credential resolves only the selected source, once per prepared model call.
// It has no fallback, including the retired ~/.inf/config or legacy host files.
// Loading or inspecting configuration never calls it. The returned key is
// ephemeral: callers must not retain it in configuration or request evidence.
func (p Provider) Credential(ctx context.Context, lookup func(string) (string, bool)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := p.validateCredentialSource(); err != nil {
		return "", err
	}
	var value string
	var err error
	switch p.credentialSource() {
	case "none", "inference-net":
		return "", nil
	case "env":
		if lookup == nil {
			return "", errors.New("credential environment lookup is required")
		}
		var ok bool
		value, ok = lookup(p.CredentialEnv)
		if !ok {
			return "", errors.New("credential environment variable is unset")
		}
	case "file":
		value, err = readCredentialFile(p.CredentialFile)
	case "command":
		value, err = runCredentialCommand(ctx, *p.CredentialCommand, lookup)
	}
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return validateCredential(value)
}

func validateCredential(value string) (string, error) {
	if len(value) > maxCredentialBytes {
		return "", errors.New("provider credential exceeds size limit")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("provider credential is empty")
	}
	for i := range len(value) {
		if value[i] < '!' || value[i] > '~' {
			return "", errors.New("provider credential must be a single printable key")
		}
	}
	return value, nil
}

func readCredentialFile(path string) (string, error) {
	directory, err := openCredentialDirectory(filepath.Dir(path))
	if err != nil {
		return "", errors.New("credential file directory is unavailable or unsafe")
	}
	defer directory.Close()
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errors.New("credential file is unavailable or unsafe")
	}
	file := os.NewFile(uintptr(fd), "credential")
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !owned(before) || before.Mode().Perm()&0o077 != 0 || before.Size() > maxCredentialBytes {
		return "", errors.New("credential file must be a bounded private regular file owned by this user")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return "", errors.New("credential file could not be read")
	}
	after, err := file.Stat()
	if err != nil || !owned(after) || after.Mode().Perm()&0o077 != 0 || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(raw)) != before.Size() {
		return "", errors.New("credential file changed while being read")
	}
	return string(raw), nil
}

// Walk from a fixed root descriptor: intermediate and final symlinks are both
// rejected, and renaming an ancestor cannot redirect the subsequent file open.
// System ancestors may be owned by root; the immediate parent must be ours and
// may not grant other users write access.
func openCredentialDirectory(path string) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, err
	}
	for component := range strings.SplitSeq(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, err := unix.Openat(fd, component, flags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	directory := os.NewFile(uintptr(fd), "credential directory")
	info, err := directory.Stat()
	if err != nil || !owned(info) || info.Mode().Perm()&0o022 != 0 {
		_ = directory.Close()
		return nil, errors.New("unsafe credential directory")
	}
	return directory, nil
}
