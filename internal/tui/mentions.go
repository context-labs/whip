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

// mentionPaths scans text for @path tokens and returns each resolved absolute
// path with its line-range suffix ("" when the token had no #start-end).
// Unlike a plain whitespace split, a token that doesn't resolve as-is is
// extended word-by-word (greedy: longest first) so a space-containing path
// like a macOS screenshot name resolves to the real file. Backslash-escaped
// spaces (Finder drag) are unescaped before resolution.
func mentionPaths(text string) [][2]string {
	var out [][2]string
	fields := strings.Fields(text)
	for i := 0; i < len(fields); i++ {
		tok := fields[i]
		if !strings.HasPrefix(tok, "@") || len(tok) < 2 {
			continue
		}
		// Try the bare token first, then extend greedily with the next words
		// (space-joined) until one resolves; the longest match wins.
		for j := i; j < len(fields); j++ {
			cand := strings.Join(fields[i:j+1], " ") // j==i is the bare token
			cand = cand[1:]                          // drop the @
			cand = strings.TrimRight(cand, ".,;:!?)\"'")
			lines := ""
			if m := rangeRe.FindStringSubmatch(cand); m != nil {
				cand = strings.TrimSuffix(cand, m[0])
				lines = " (lines " + m[1]
				if m[2] != "" {
					lines += "-" + m[2]
				}
				lines += ")"
			}
			if abs, ok := resolveMentionPath(unescapePath(cand)); ok {
				out = append(out, [2]string{abs, lines})
				i = j // consumed the extension words
				break
			}
		}
	}
	return out
}

// unescapePath turns a shell/Finder-escaped path ("a\ b.png") back into its
// real form ("a b.png"). Only backslash-space is unescaped; other escapes are
// left alone (a path with a literal backslash is rare and unambiguous).
func unescapePath(s string) string {
	return strings.ReplaceAll(s, `\ `, " ")
}

// expandMentions finds @file tokens (any path: relative, absolute, or ~, a
// bare word that uniquely fuzzy-matches a file under the cwd, each with
// optional #start-end line ranges) and appends a pointer note. File contents
// are never inlined — the model inspects tagged files with its own tools.
// Space-containing paths (macOS screenshot names) resolve via mentionPaths.
func expandMentions(text string) string {
	var notes []string
	for _, m := range mentionPaths(text) {
		notes = append(notes, m[0]+m[1]) // path + optional " (lines a-b)"
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
// Space-containing paths (macOS screenshot names) resolve via mentionPaths.
func imageParts(text string) ([]llm.ContentPart, string) {
	var parts []llm.ContentPart
	var names []string
	for _, m := range mentionPaths(text) {
		abs := m[0]
		if !imageExtsForMention[strings.ToLower(filepath.Ext(abs))] {
			continue
		}
		data, err := os.ReadFile(abs) //nolint:gosec // G304: abs is the user-@mentioned path, resolved to the workspace
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(abs)), ".")
		parts = append(parts, llm.ImagePart(ext, data))
		names = append(names, abs)
	}
	if len(parts) == 0 {
		return nil, ""
	}
	return parts, "\n\n[note: the user attached image(s): " + strings.Join(names, "; ") +
		" — they are inlined above as vision input]"
}
