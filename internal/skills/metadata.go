package skills

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParsePromptMetadata parses only a complete frontmatter block of at most
// 64 KiB, including its delimiters. It performs no filesystem reads. Known
// catalog fields require scalar values; unrelated nested metadata is ignored.
// This intentionally supports the existing scalar subset, not general YAML.
func ParsePromptMetadata(path string, data []byte) (Skill, error) {
	if len(data) > maxPromptMetadataBytes {
		return Skill{}, fmt.Errorf("%s: frontmatter exceeds %d bytes", path, maxPromptMetadataBytes)
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return Skill{}, fmt.Errorf("%s: frontmatter must be UTF-8 without NUL", path)
	}
	first, closed := true, false
	for line := range strings.Lines(string(data)) {
		if closed {
			return Skill{}, fmt.Errorf("%s: metadata parser received body text", path)
		}
		if first {
			if strings.TrimRight(line, "\r\n") != "---" {
				return Skill{}, fmt.Errorf("%s: no frontmatter", path)
			}
			first = false
		} else if strings.TrimRight(line, "\r\n") == "---" {
			closed = true
		}
	}
	if !closed {
		return Skill{}, fmt.Errorf("%s: missing closing frontmatter delimiter", path)
	}
	skill, err := parseMetadataMode(path, bytes.NewReader(data), true)
	if err != nil {
		return Skill{}, err
	}
	if skill.Name == "" {
		skill.Name = filepath.Base(filepath.Dir(path))
	}
	if !utf8.ValidString(skill.Name+skill.Description) || strings.ContainsRune(skill.Name+skill.Description, 0) {
		return Skill{}, fmt.Errorf("%s: decoded metadata must be UTF-8 without NUL", path)
	}
	if warning := validate(skill); warning != "" {
		return Skill{}, fmt.Errorf("%s: %s", path, warning)
	}
	return skill, nil
}

func validateMetadataScalar(key, value string) error {
	value = strings.TrimSpace(value)
	if key == "disable-model-invocation" {
		if value != "true" && value != "false" {
			return errors.New("disable-model-invocation must be true or false")
		}
		return nil
	}
	if isBlockScalarIndicator(value) {
		chomp, indent := false, false
		for _, flag := range value[1:] {
			if flag == '+' || flag == '-' {
				if chomp {
					return fmt.Errorf("%s has an invalid block scalar header", key)
				}
				chomp = true
			} else {
				if indent || flag == '0' {
					return fmt.Errorf("%s has an invalid block scalar header", key)
				}
				indent = true
			}
		}
		return nil
	}
	if value == "" || strings.ContainsAny(value[:1], "[{&*!>|%") || value == "null" || value == "~" {
		return fmt.Errorf("%s requires a string scalar", key)
	}
	_, err := decodeMetadataScalar(value)
	return err
}

func decodeMetadataScalar(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", errors.New("invalid quoted metadata scalar")
		}
		return decoded, nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", errors.New("unterminated quoted metadata scalar")
		}
		body := value[1 : len(value)-1]
		if strings.Contains(strings.ReplaceAll(body, "''", ""), "'") {
			return "", errors.New("invalid quoted metadata scalar")
		}
		return strings.ReplaceAll(body, "''", "'"), nil
	}
	return value, nil
}
