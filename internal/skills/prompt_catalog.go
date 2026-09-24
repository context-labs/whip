package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxPromptMetadataBytes = 64 << 10
	maxPromptSkills        = 1024
	maxPromptDirEntries    = 8192
)

// LoadPromptCatalog discovers the same immediate skill directories as Scan,
// but bounds metadata reads and reports failures instead of omitting them from
// an authoritative prompt. Directory precedence and disabled-skill behavior
// remain the same as Scan. Missing optional directories and SKILL.md are fine.
func LoadPromptCatalog(dirs ...string) ([]Skill, error) {
	return LoadPromptCatalogWithAccess(nil, dirs...)
}

// LoadPromptCatalogWithAccess optionally checks each original directory/file
// path before reading it. The caller resolves aliases when deciding access;
// a denied location is omitted, while an access-check error fails discovery.
func LoadPromptCatalogWithAccess(allow func(string) (bool, error), dirs ...string) ([]Skill, error) {
	var result []Skill
	entriesRead := 0
	for _, dir := range dirs {
		skills, err := loadPromptDirectory(dir, &entriesRead, allow)
		if err != nil {
			return nil, err
		}
		if len(result)+len(skills) > maxPromptSkills {
			return nil, fmt.Errorf("skill catalog exceeds %d skills", maxPromptSkills)
		}
		result = append(result, skills...)
	}
	return result, nil
}

func loadPromptDirectory(dir string, entriesRead *int, allow func(string) (bool, error)) ([]Skill, error) {
	_, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skill directory %s: %w", dir, err)
	}
	if allow != nil {
		allowed, err := allow(dir)
		if err != nil || !allowed {
			return nil, err
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("skill directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill directory %s: expected a directory", dir)
	}
	f, err := os.Open(dir) //nolint:gosec // Skill discovery intentionally reads the directories selected by the local caller.
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skill directory %s: %w", dir, err)
	}
	defer f.Close()
	var result []Skill
	for {
		entries, err := f.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("skill directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			*entriesRead += 1
			if *entriesRead > maxPromptDirEntries {
				return nil, fmt.Errorf("skill catalog exceeds %d directory entries", maxPromptDirEntries)
			}
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("skill %s: %w", path, err)
			}
			if allow != nil {
				allowed, err := allow(path)
				if err != nil {
					return nil, err
				}
				if !allowed {
					continue
				}
			}
			skill, loadErr := loadPromptMetadata(path)
			if loadErr != nil {
				return nil, loadErr
			}
			result = append(result, skill)
			if len(result) > maxPromptSkills {
				return nil, fmt.Errorf("skill directory %s exceeds %d skills", dir, maxPromptSkills)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	slices.SortFunc(result, func(a, b Skill) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}

func loadPromptMetadata(path string) (Skill, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Skill{}, fmt.Errorf("skill %s: expected a regular file", path)
	}
	f, err := os.Open(path) //nolint:gosec // The path names SKILL.md in a directory discovered from the configured skill roots.
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPromptMetadataBytes+1))
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", path, err)
	}
	end := 0
	closed := false
	for line := range strings.Lines(string(data)) {
		if end == 0 && strings.TrimSpace(line) != "---" {
			return Skill{}, fmt.Errorf("skill %s: no frontmatter", path)
		}
		if end > 0 && strings.TrimSpace(line) == "---" {
			end += len(line)
			closed = true
			break
		}
		end += len(line)
	}
	if end > maxPromptMetadataBytes {
		return Skill{}, fmt.Errorf("skill %s: frontmatter exceeds %d bytes", path, maxPromptMetadataBytes)
	}
	if !closed {
		return Skill{}, fmt.Errorf("skill %s: missing closing frontmatter delimiter", path)
	}
	if !utf8.Valid(data[:end]) {
		return Skill{}, fmt.Errorf("skill %s: frontmatter is not valid UTF-8", path)
	}
	skill, err := parseMetadata(path, bytes.NewReader(data[:end]))
	if err != nil {
		return Skill{}, err
	}
	if skill.Name == "" {
		skill.Name = filepath.Base(filepath.Dir(path))
	}
	skill.Warning = validate(skill)
	return skill, nil
}
