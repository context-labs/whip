package instruction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
)

type Skill struct {
	Name, Description string
	Disabled          bool
	Source            session.InstructionSource
}

type SkillCatalog struct {
	Skills  []Skill
	Sources []session.InstructionSource
}

// orderedRoots preserves host policy order and puts the local chain last without
// changing the caller's slice. Validation never probes a root's filesystem.
func orderedRoots(roots []Root) ([]Root, error) {
	if len(roots) > session.MaxSkillRoots+1 {
		return nil, errors.New("too many instruction roots")
	}
	ordered := make([]Root, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	var local *Root
	for _, root := range roots {
		key := root.Scope() + ":" + root.ID
		if seen[key] || root.FS == nil {
			return nil, errors.New("instruction roots require unique scopes and authorized directories")
		}
		seen[key] = true
		if root.ID != "" {
			if err := session.ValidateID(root.ID); err != nil {
				return nil, err
			}
		}
		if root.Scope() == "host" {
			ordered = append(ordered, root)
			continue
		}
		if local != nil {
			return nil, errors.New("instruction roots require one workspace or project chain")
		}
		if root.Scope() == "project" {
			if root.ID == "" || len(root.ProjectDirectories) > MaxProjectDirectories {
				return nil, errors.New("project chain requires a named boundary and at most 128 directories")
			}
			for i, path := range root.ProjectDirectories {
				if len(path) > 4096 || !utf8.ValidString(path) || !fs.ValidPath(path) || strings.ContainsAny(path, "\\\x00") || (i == 0 && path != ".") || (i > 0 && filepath.ToSlash(filepath.Dir(path)) != root.ProjectDirectories[i-1]) {
					return nil, errors.New("project directories must form one bounded boundary-relative chain")
				}
			}
		}
		local = &root
	}
	if len(ordered) > session.MaxSkillRoots {
		return nil, errors.New("too many named instruction roots")
	}
	if local != nil {
		ordered = append(ordered, *local)
	}
	return ordered, nil
}

// Catalog shares winners across discovery, invocation and client inspection.
// The last sorted path in the last root wins, including disabled skills. The
// project chain or workspace is last. Sources retain all metadata, including losers.
func Catalog(ctx context.Context, roots []Root) (SkillCatalog, error) {
	if err := ctx.Err(); err != nil {
		return SkillCatalog{}, err
	}
	roots, err := orderedRoots(roots)
	if err != nil {
		return SkillCatalog{}, err
	}
	var catalog SkillCatalog
	var bounds catalogBounds
	byName := make(map[string]Skill)
	for _, root := range roots {
		directories := []string{skillDirectory}
		if root.Scope() == "host" {
			directories = []string{"."}
		} else if root.Scope() == "project" {
			directories = make([]string, len(root.ProjectDirectories))
			for i, directory := range root.ProjectDirectories {
				directories[i] = filepath.Join(directory, skillDirectory)
			}
		}
		for _, directory := range directories {
			metadata, sources, err := loadSkills(ctx, root, directory, &bounds)
			if err != nil {
				return SkillCatalog{}, err
			}
			catalog.Sources = append(catalog.Sources, sources...)
			for i, skill := range metadata {
				byName[skill.Name] = Skill{Name: skill.Name, Description: skill.Description, Disabled: skill.DisableModelInvocation, Source: sources[i]}
			}
		}
	}
	for _, skill := range byName {
		catalog.Skills = append(catalog.Skills, skill)
	}
	slices.SortFunc(catalog.Skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return catalog, nil
}

// appendCatalog advertises the authorized skill reader rather than filesystem
// access. Paths are provenance relative to their root, never host paths.
func appendCatalog(text *strings.Builder, catalog []Skill) error {
	opened := false
	for _, skill := range catalog {
		if skill.Disabled {
			continue
		}
		if !opened {
			if err := appendText(text, "\n\nRead skill bodies with skills.read. Pass scope and root_id (the values shown below), name (the exact skill name), offset \"0\", and length up to 65536. The initial sha256 may be omitted. For subsequent pages, use next_offset and the returned sha256.\n<available_skills>\n"); err != nil {
				return err
			}
			opened = true
		}
		rootID := "null"
		if skill.Source.RootID != nil {
			rootID = strconv.Quote(*skill.Source.RootID)
		}
		block := "<skill>\n<name>" + html.EscapeString(skill.Name) + "</name>\n<description>" + html.EscapeString(skill.Description) + "</description>\n<scope>" + skill.Source.Scope + "</scope>\n<root_id>" + html.EscapeString(rootID) + "</root_id>\n<location>" + html.EscapeString(skill.Source.Path) + "</location>\n</skill>\n"
		if err := appendText(text, block); err != nil {
			return err
		}
	}
	if opened {
		return appendText(text, "</available_skills>")
	}
	return nil
}

// InvokedNames recognizes whitespace-separated $name tokens in direct text
// parts only. Names retain their case, first occurrence and legacy trailing
// punctuation rules. It neither reads attachments nor changes submitted parts.
func InvokedNames(parts []session.Part) []string {
	var names []string
	seen := map[string]bool{}
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		for token := range strings.FieldsSeq(part.Text) {
			if !strings.HasPrefix(token, "$") {
				continue
			}
			name := strings.TrimRight(strings.TrimPrefix(token, "$"), ".,;:!?)\"'")
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// ReadSkill consumes an explicitly selected body only through the supplied
// authorized root. Selection is metadata, not authority. Metadata must still
// match the selection exactly; body edits alone are read fresh and audited.
func ReadSkill(ctx context.Context, root *os.Root, selected Skill) (string, session.InstructionSource, error) {
	if err := ctx.Err(); err != nil {
		return "", session.InstructionSource{}, err
	}
	if root == nil {
		return "", session.InstructionSource{}, errors.New("skill read requires an authorized root")
	}
	if err := selected.Source.Validate(); err != nil {
		return "", session.InstructionSource{}, err
	}
	if selected.Source.Kind != "skill_metadata" {
		return "", session.InstructionSource{}, errors.New("skill selection must identify catalog metadata")
	}
	file, err := root.OpenFile(selected.Source.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", session.InstructionSource{}, fmt.Errorf("open invoked skill %s: %w", selected.Name, withoutFilesystemPath(err))
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", session.InstructionSource{}, withoutFilesystemPath(err)
	}
	if !info.Mode().IsRegular() {
		return "", session.InstructionSource{}, errors.New("invoked skill must be a regular file")
	}
	data, err := readComplete(file, info, session.MaxInvokedSkillBytes)
	if err != nil {
		return "", session.InstructionSource{}, err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", session.InstructionSource{}, errors.New("invoked skill must be UTF-8 without NUL")
	}
	metadata, err := readMetadata(ctx, bytes.NewReader(data))
	if err != nil {
		return "", session.InstructionSource{}, err
	}
	parsed, err := skills.ParsePromptMetadata(selected.Source.Path, metadata)
	if err != nil {
		return "", session.InstructionSource{}, err
	}
	rootID := ""
	if selected.Source.RootID != nil {
		rootID = *selected.Source.RootID
	}
	observed := source("skill_metadata", selected.Source.Scope, rootID, selected.Source.Path, metadata)
	// Attribution was validated above and copied from the selection; compare
	// source values without making pointer identity part of the fingerprint.
	observed.RootID = selected.Source.RootID
	if observed != selected.Source || parsed.Name != selected.Name || parsed.Description != selected.Description || parsed.DisableModelInvocation != selected.Disabled {
		return "", session.InstructionSource{}, errors.New("invoked skill metadata changed after selection")
	}
	if err := ctx.Err(); err != nil {
		return "", session.InstructionSource{}, err
	}
	return string(data), source("invoked_skill", selected.Source.Scope, rootID, selected.Source.Path, data), nil
}
