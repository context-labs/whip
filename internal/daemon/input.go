package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/skills"
)

const (
	maxMentionImageBytes = 20 << 20
	maxInvokedSkillBytes = 256 << 10
)

var (
	mentionRange = regexp.MustCompile(`#(\d+)(?:-(\d+))?$`)
	imageExt     = map[string]string{
		".png": "png", ".jpg": "jpg", ".jpeg": "jpg", ".gif": "gif",
		".webp": "webp", ".bmp": "bmp",
	}
)

// prepareAuthoredInput is the shared root/descendant expansion boundary. The
// terminal only offers completions; the daemon resolves paths and skills
// against the effective session before anything reaches model context.
func (session *AgentSession) prepareAuthoredInput(ctx context.Context, input string, parts []llm.ContentPart) (string, []llm.ContentPart, error) {
	input, parts, err := session.expandMentionedFiles(ctx, input, parts)
	if err != nil {
		return "", nil, err
	}
	input, err = session.expandInvokedSkills(input)
	return input, parts, err
}

func (session *AgentSession) expandInvokedSkills(input string) (string, error) {
	available := skills.Scan(skills.DirsFor(session.agent.WorkingDir)...)
	byName := make(map[string]skills.Skill, len(available))
	for _, skill := range available {
		byName[skill.Name] = skill
	}
	var sections []string
	seen := map[string]bool{}
	for _, token := range strings.Fields(input) {
		name := strings.TrimRight(strings.TrimPrefix(token, "$"), ".,;:!?)\"'")
		skill, ok := byName[name]
		if !strings.HasPrefix(token, "$") || !ok || seen[name] {
			continue
		}
		seen[name] = true
		body, err := os.ReadFile(skill.Path) //nolint:gosec // path came from configured skill roots
		if err != nil {
			return "", fmt.Errorf("read invoked skill %s: %w", name, err)
		}
		if len(body) > maxInvokedSkillBytes {
			return "", fmt.Errorf("invoked skill %s exceeds the %d-byte limit", name, maxInvokedSkillBytes)
		}
		sections = append(sections, fmt.Sprintf("<invoked_skill name=%q location=%q>\n%s\n</invoked_skill>", name, skill.Path, body))
	}
	if len(sections) > 0 {
		input += "\n\n" + strings.Join(sections, "\n\n")
	}
	return input, nil
}

func (session *AgentSession) expandMentionedFiles(ctx context.Context, input string, parts []llm.ContentPart) (string, []llm.ContentPart, error) {
	var notes []string
	seen := map[string]bool{}
	imageBytes := 0
	for _, token := range strings.Fields(input) {
		if !strings.HasPrefix(token, "@") || len(token) < 2 {
			continue
		}
		value := strings.TrimRight(token[1:], ".,;:!?)\"'")
		lineRange := ""
		if match := mentionRange.FindStringSubmatch(value); match != nil {
			value = strings.TrimSuffix(value, match[0])
			lineRange = " (lines " + match[1]
			if match[2] != "" {
				lineRange += "-" + match[2]
			}
			lineRange += ")"
		}
		path, ok := resolveSessionMention(session.agent.WorkingDir, value)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		if err := session.authorizeMention(ctx, path); err != nil {
			return "", nil, errors.New("this agent cannot inspect mentioned files without a file capability")
		}
		format, image := imageExt[strings.ToLower(filepath.Ext(path))]
		if image && session.agent.Vision {
			data, err := os.ReadFile(path) //nolint:gosec // canonical workspace path resolved below
			if err != nil {
				return "", nil, fmt.Errorf("read mentioned image: %w", err)
			}
			imageBytes += len(data)
			if imageBytes > maxMentionImageBytes {
				return "", nil, fmt.Errorf("mentioned images exceed the %d-byte limit", maxMentionImageBytes)
			}
			parts = append(parts, llm.ImagePart(format, data))
			notes = append(notes, path+" (attached image)")
			continue
		}
		notes = append(notes, path+lineRange)
	}
	if len(notes) > 0 {
		input += "\n\n[note: the user tagged " + strings.Join(notes, "; ") + " — inspect regular files with the files module as needed]"
	}
	return input, parts, nil
}

func (session *AgentSession) authorizeMention(ctx context.Context, path string) error {
	if session.root != nil {
		return session.root.store.AuthorizeCapability(ctx, session.root.ID(), session.id, session.authority.Files, "read", path)
	}
	// Detached root sessions exist only in focused unit tests. A detached child
	// still has to declare the semantic read capability.
	if session.parentID == "" || slices.Contains(session.capabilities, "read") {
		return nil
	}
	return errors.New("read capability is unavailable")
}

func resolveSessionMention(root, value string) (string, bool) {
	if root == "" || value == "" {
		return "", false
	}
	path := value
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = home + path[1:]
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if resolved, ok := canonicalWorkspaceFile(root, path); ok {
		return resolved, true
	}
	if strings.ContainsAny(value, `/\\`) {
		return "", false
	}
	var matches []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || len(matches) > 1 {
			return nil
		}
		if path != root && entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(value)) {
			return nil
		}
		if resolved, ok := canonicalWorkspaceFile(root, path); ok {
			matches = append(matches, resolved)
		}
		return nil
	})
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func canonicalWorkspaceFile(root, path string) (string, bool) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		return "", false
	}
	relative, err := filepath.Rel(canonicalRoot, canonical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return canonical, true
}
