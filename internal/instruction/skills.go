package instruction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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

// Catalog uses one winner for each exact name across discovery, explicit
// invocation and client inspection. The last sorted path wins, including a
// disabled skill. Sources retain every consumed metadata block, even losers.
// A nil root performs no filesystem reads and returns an empty catalog.
func Catalog(ctx context.Context, root *os.Root) (SkillCatalog, error) {
	if err := ctx.Err(); err != nil {
		return SkillCatalog{}, err
	}
	if root == nil {
		return SkillCatalog{}, nil
	}
	metadata, sources, err := loadSkills(ctx, root)
	if err != nil {
		return SkillCatalog{}, err
	}
	byName := make(map[string]Skill, len(metadata))
	for i, skill := range metadata {
		byName[skill.Name] = Skill{Name: skill.Name, Description: skill.Description, Disabled: skill.DisableModelInvocation, Source: sources[i]}
	}
	catalog := SkillCatalog{Sources: sources, Skills: make([]Skill, 0, len(byName))}
	for _, skill := range byName {
		catalog.Skills = append(catalog.Skills, skill)
	}
	slices.SortFunc(catalog.Skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return catalog, nil
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
		return "", session.InstructionSource{}, errors.New("skill read requires an authorized workspace root")
	}
	if err := selected.Source.Validate(); err != nil {
		return "", session.InstructionSource{}, err
	}
	if selected.Source.Kind != "skill_metadata" {
		return "", session.InstructionSource{}, errors.New("skill selection must identify catalog metadata")
	}
	file, err := root.OpenFile(selected.Source.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", session.InstructionSource{}, fmt.Errorf("open invoked skill %s: %w", selected.Name, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", session.InstructionSource{}, err
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
	if source("skill_metadata", selected.Source.Path, metadata) != selected.Source || parsed.Name != selected.Name || parsed.Description != selected.Description || parsed.DisableModelInvocation != selected.Disabled {
		return "", session.InstructionSource{}, errors.New("invoked skill metadata changed after selection")
	}
	if err := ctx.Err(); err != nil {
		return "", session.InstructionSource{}, err
	}
	return string(data), source("invoked_skill", selected.Source.Path, data), nil
}
