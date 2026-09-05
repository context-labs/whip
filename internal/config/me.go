package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxMeInstructionBytes bounds the standing instruction file before comments
// are removed. An oversized file is an error, never a partially applied rule.
const MaxMeInstructionBytes = 64 << 10

// me.md: the user's standing instructions for the agent. The file is
// APPENDED to the built-in operating rules (they carry the safety rails —
// secrets review, no force-push — and never go away), so the seed is a
// commented template, not a copy of the defaults that would silently
// diverge. /me opens the file in $EDITOR.

// MeSeed is what ~/.whip/me.md starts with.
const MeSeed = `# Your standing instructions for whip — appended to every session's
# system prompt, after the built-in operating rules. Lines starting with #
# are comments. Edit freely; /me opens this file.

# Examples:
# - Always run tests with pnpm, never npm.
# - I review every commit message before you commit — always show me the message first.
# - Never touch files under deploy/prod/ without asking.
`

// MePath returns ~/.whip/me.md (seeding the template on first run); "" when
// the home dir is unavailable.
func MePath() string {
	path, _ := mePath()
	return path
}

func mePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(dir, "me.md"))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Concurrent turn starts must not overwrite a file another caller (or
		// the user) created after the initial stat.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		_, writeErr := f.WriteString(MeSeed)
		closeErr := f.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	} else if err != nil {
		return "", err
	}
	return path, nil
}

// MeInstructions loads the user's standing instructions from ~/.whip/me.md,
// comments and blank lines stripped. "" means nothing to append.
func MeInstructions() string {
	instructions, _ := LoadMeInstructions()
	return instructions
}

// LoadMeInstructions preserves MeInstructions' comment convention while
// reporting failures to callers that must not silently omit user constraints.
func LoadMeInstructions() (string, error) {
	path, err := mePath()
	if err != nil {
		return "", fmt.Errorf("standing instructions: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("standing instructions %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("standing instructions %s: expected a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("standing instructions %s: %w", path, err)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return "", fmt.Errorf("standing instructions %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("standing instructions %s: expected a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxMeInstructionBytes+1))
	if err != nil {
		return "", fmt.Errorf("standing instructions %s: %w", path, err)
	}
	if len(data) > MaxMeInstructionBytes {
		return "", fmt.Errorf("standing instructions %s exceeds %d bytes", path, MaxMeInstructionBytes)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("standing instructions %s is not valid UTF-8", path)
	}
	var lines []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}
