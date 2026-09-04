package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/skills"
)

// expandSkills appends an invocation note for $skill-name tokens (codex-style).
// Unknown $tokens are left alone.
func expandSkills(text string, sk []skills.Skill) string {
	var used []string
	for tok := range strings.FieldsSeq(text) {
		if !strings.HasPrefix(tok, "$") || len(tok) < 2 {
			continue
		}
		name := strings.TrimRight(tok[1:], ".,;:!?)\"'")
		for _, s := range sk {
			if s.Name == name {
				used = append(used, s.Name+" ("+s.Path+")")
				break
			}
		}
	}
	if len(used) == 0 {
		return text
	}
	return text + "\n\n[note: the user invoked skill(s): " + strings.Join(used, "; ") +
		" — read each SKILL.md with the read tool and follow its instructions for this request]"
}

var rangeRe = regexp.MustCompile(`#(\d+)(?:-(\d+))?$`)

// expandMentions finds @file tokens (any path: relative, absolute, or ~, a
// bare word that uniquely fuzzy-matches a file under the cwd, each with
// optional #start-end line ranges) and appends a pointer note. File contents
// are never inlined — the model inspects tagged files with its own tools.
func expandMentions(text string) string {
	var notes []string
	for tok := range strings.FieldsSeq(text) {
		if !strings.HasPrefix(tok, "@") || len(tok) < 2 {
			continue
		}
		p := strings.TrimRight(tok[1:], ".,;:!?)\"'")
		lines := ""
		if m := rangeRe.FindStringSubmatch(p); m != nil {
			p = strings.TrimSuffix(p, m[0])
			lines = " (lines " + m[1]
			if m[2] != "" {
				lines += "-" + m[2]
			}
			lines += ")"
		}
		// Real paths stat as-is; a bare word may uniquely fuzzy-match the
		// recursive cwd index ("@roadmap" → docs/roadmap.md). Anything else
		// is left alone.
		abs, ok := resolveMentionPath(p)
		if !ok {
			continue
		}
		notes = append(notes, abs+lines)
	}
	if len(notes) == 0 {
		return text
	}
	return text + "\n\n[note: the user tagged " + strings.Join(notes, "; ") +
		" — contents are not inlined; inspect with your tools as needed]"
}

// imageExtsForMention are the @-taggable image formats we inline as vision
// parts, keyed by file extension.
var imageExtsForMention = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
}

// imageParts finds @image tags in text, reads each image, and returns the
// vision parts plus a note appended to the text naming the attached images.
// Unlike text @files (pointer notes; the model inspects them with tools),
// images must be inlined — the model has no way to view a local image itself.
func imageParts(text string) ([]llm.ContentPart, string) {
	var parts []llm.ContentPart
	var names []string
	for tok := range strings.FieldsSeq(text) {
		if !strings.HasPrefix(tok, "@") || len(tok) < 2 {
			continue
		}
		p := strings.TrimRight(tok[1:], ".,;:!?)\"'")
		if !imageExtsForMention[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		abs, ok := resolveMentionPath(p)
		if !ok {
			continue
		}
		data, err := os.ReadFile(abs) //nolint:gosec // G304: abs is the user-@mentioned path, resolved to the workspace
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(abs)), ".")
		// Normalize before inlining: a HiDPI screenshot at full resolution
		// costs 4–10× the tokens of the capped re-encode, with no legibility
		// gain the model can use.
		ext, data = llm.NormalizeImage(ext, data)
		parts = append(parts, llm.ImagePart(ext, data))
		names = append(names, abs)
	}
	if len(parts) == 0 {
		return nil, ""
	}
	return parts, "\n\n[note: the user attached image(s): " + strings.Join(names, "; ") +
		" — they are inlined above as vision input]"
}
