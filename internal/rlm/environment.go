package rlm

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/skills"
)

const (
	maxProjectInstructionBytes = 64 << 10
	maxEnvironmentPromptBytes  = 1 << 20
)

// PromptOptions describes the environment authorized by the caller. ProjectRoots
// are ancestor boundaries, not grants: only the chain from an applicable root
// through WorkingDirectory is read. Unrelated roots are ignored; with no
// applicable root only WorkingDirectory is considered.
type PromptOptions struct {
	WorkingDirectory string
	ProjectRoots     []string
	Identity         Identity
	Now              time.Time
	Platform         string
	Username         string
	// ProjectDirectoryAllowed filters automatic project context using canonical
	// paths. Nil trusts the caller's project boundaries. User rules and user
	// skill directories remain global context.
	ProjectDirectoryAllowed func(string) (bool, error)
	// Nil discovers scoped project and user skills. A non-nil empty slice
	// disables skill discovery; explicit entries preserve caller ordering.
	SkillDirs []string
}

// PromptSource identifies text actually inserted into an assembled prompt.
// Bytes counts source content before framing or escaping. Scope is the
// applicable directory for project rules and "all" for global instructions.
type PromptSource struct {
	Kind  string `json:"kind"`
	Path  string `json:"path,omitempty"`
	Scope string `json:"scope"`
	Bytes int    `json:"bytes"`
}

// PromptSnapshot is the complete prompt and the sources used for one turn.
// Skills also includes explicitly invokable entries omitted from the visible
// catalog by disable-model-invocation, so input expansion uses the same scan.
type PromptSnapshot struct {
	Prompt           string
	WorkingDirectory string
	Sources          []PromptSource
	Skills           []skills.Skill
	AppliedAt        time.Time
}

const operatingRules = `Operating rules:
- When the user tags a file with @, inspect the listed path with files.read.
- Bias toward acting on reasonable assumptions. After repeated failures on one blocker, escalate it plainly instead of looping.
- For child collaboration, use messages and the configured report mode; do not assume the parent receives the full child transcript.
- Git hygiene: inspect staged changes for secrets, stage intentional files only, and never force-push.

Instruction scope and precedence:
- Explicit user instructions are authoritative over project and skill guidance. Standing instructions below are user rules.
- Project instructions apply to files in their stated directory and its descendants. More specific directories take precedence; at the same directory AGENTS.md takes precedence over CLAUDE.md.
- Before working in a narrower authorized subtree or another repository, use files.list/files.read to check for its CLAUDE.md and AGENTS.md and follow applicable rules. The catalog does not contain every nested instruction file.
- Instructions and skills do not expand filesystem access, permissions, or delegation authority.`

// ComposePrompt assembles normal root and child prompts at a turn boundary.
// Every read is bounded; an applicable source that cannot be loaded fails the
// assembly, so callers cannot accidentally run with only part of a user rule.
func ComposePrompt(options PromptOptions) (PromptSnapshot, error) {
	cwd, err := filepath.Abs(options.WorkingDirectory)
	if err != nil {
		return PromptSnapshot{}, fmt.Errorf("prompt working directory: %w", err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return PromptSnapshot{}, fmt.Errorf("prompt working directory %s: %w", options.WorkingDirectory, err)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return PromptSnapshot{}, fmt.Errorf("prompt working directory %s: %w", cwd, err)
	}
	if !info.IsDir() {
		return PromptSnapshot{}, fmt.Errorf("prompt working directory %s: expected a directory", cwd)
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if options.Platform == "" {
		options.Platform = runtime.GOOS
	}
	if options.Username == "" {
		options.Username = "unknown"
		if current, err := user.Current(); err == nil && current.Username != "" {
			options.Username = current.Username
		}
	}
	snapshot := PromptSnapshot{WorkingDirectory: cwd, AppliedAt: options.Now}
	var prompt strings.Builder
	appendSource := func(kind, path, scope, text string) error {
		if text == "" {
			return nil
		}
		if prompt.Len()+len(text)+2 > maxEnvironmentPromptBytes {
			return fmt.Errorf("environment prompt exceeds %d bytes while adding %s %s", maxEnvironmentPromptBytes, kind, path)
		}
		if prompt.Len() > 0 {
			prompt.WriteString("\n\n")
		}
		prompt.WriteString(text)
		snapshot.Sources = append(snapshot.Sources, PromptSource{Kind: kind, Path: path, Scope: scope, Bytes: len(text)})
		return nil
	}
	if err := appendSource("runtime", "", "all", BuildPrompt(cwd, nil)); err != nil {
		return PromptSnapshot{}, err
	}
	if err := appendSource("identity", "", "all", strings.TrimSpace(IdentityBlock(options.Identity))); err != nil {
		return PromptSnapshot{}, err
	}
	if err := appendSource("operating_rules", "", "all", operatingRules); err != nil {
		return PromptSnapshot{}, err
	}
	environment := fmt.Sprintf("Environment:\n<env>\n  Platform: %s\n  Current date/time: %s\n  User: %s\n</env>",
		options.Platform, options.Now.Format("Mon Jan 2, 2006 15:04:05 MST (UTC-07:00)"), options.Username)
	if err := appendSource("environment", "", cwd, environment); err != nil {
		return PromptSnapshot{}, err
	}
	chain, err := authorizedProjectChain(cwd, options)
	if err != nil {
		return PromptSnapshot{}, err
	}
	for _, directory := range chain {
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			path := filepath.Join(directory, name)
			content, err := readProjectInstructions(path, chain[0])
			if err != nil {
				return PromptSnapshot{}, err
			}
			if content == "" {
				continue
			}
			framed := fmt.Sprintf("Project instructions from %q (scope %q and descendants):\n%s", path, directory, content)
			if err := appendSource("project_instructions", path, directory, framed); err != nil {
				return PromptSnapshot{}, err
			}
			snapshot.Sources[len(snapshot.Sources)-1].Bytes = len(content)
		}
	}
	snapshot.Skills, err = loadPromptSkills(options, chain)
	if err != nil {
		return PromptSnapshot{}, err
	}
	catalog := skills.PromptBlock(snapshot.Skills)
	if catalog != "" {
		if prompt.Len()+len(catalog) > maxEnvironmentPromptBytes {
			return PromptSnapshot{}, fmt.Errorf("environment prompt exceeds %d bytes while adding skill catalog", maxEnvironmentPromptBytes)
		}
		prompt.WriteString(catalog)
		for _, skill := range snapshot.Skills {
			if skill.DisableModelInvocation {
				continue
			}
			snapshot.Sources = append(snapshot.Sources, PromptSource{
				Kind: "skill", Path: skill.Path, Scope: skillPromptScope(skill.Path, chain),
				Bytes: len(skill.Name) + len(skill.Description) + len(skill.Path),
			})
		}
	}
	standing, err := config.LoadMeInstructions()
	if err != nil {
		return PromptSnapshot{}, err
	}
	if standing != "" {
		path := config.MePath()
		framed := fmt.Sprintf("Standing instructions from the user (%s — treat as user rules):\n%s", path, standing)
		if err := appendSource("standing_instructions", path, "all", framed); err != nil {
			return PromptSnapshot{}, err
		}
		snapshot.Sources[len(snapshot.Sources)-1].Bytes = len(standing)
	}
	snapshot.Prompt = prompt.String()
	return snapshot, nil
}

// LoadPromptSkills discovers the same authorized catalog used by ComposePrompt,
// without reading project instructions or replacing an explicit system prompt.
func LoadPromptSkills(options PromptOptions) ([]skills.Skill, error) {
	cwd, err := filepath.Abs(options.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if errors.Is(err, fs.ErrNotExist) {
		return loadPromptSkills(options, nil)
	}
	if err != nil {
		return nil, err
	}
	chain, err := authorizedProjectChain(cwd, options)
	if err != nil {
		return nil, err
	}
	return loadPromptSkills(options, chain)
}

func authorizedProjectChain(cwd string, options PromptOptions) ([]string, error) {
	chain := projectInstructionChain(cwd, options.ProjectRoots)
	if options.ProjectDirectoryAllowed == nil {
		return chain, nil
	}
	allowedChain := make([]string, 0, len(chain))
	for _, directory := range chain {
		allowed, err := options.ProjectDirectoryAllowed(directory)
		if err != nil {
			return nil, err
		}
		if allowed {
			allowedChain = append(allowedChain, directory)
		}
	}
	return allowedChain, nil
}

func loadPromptSkills(options PromptOptions, chain []string) ([]skills.Skill, error) {
	dirs := options.SkillDirs
	if dirs == nil {
		var err error
		dirs, err = promptSkillDirs(chain)
		if err != nil {
			return nil, err
		}
	}
	var allowRead func(string) (bool, error)
	if options.ProjectDirectoryAllowed != nil {
		allowRead = func(path string) (bool, error) {
			_, allowed, err := ResolvePromptSkill(path, options.ProjectDirectoryAllowed)
			return allowed, err
		}
	}
	return skills.LoadPromptCatalogWithAccess(allowRead, dirs...)
}

// ResolvePromptSkill checks the current canonical target before metadata or body
// reads. Only configured user skill locations are globally trusted; a project
// alias cannot acquire that trust by pointing at a user directory.
func ResolvePromptSkill(path string, allowProject func(string) (bool, error)) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false, err
	}
	if allowProject == nil {
		return canonical, true, nil
	}
	globalDirs, err := promptSkillDirs(nil)
	if err != nil {
		return "", false, err
	}
	for _, directory := range globalDirs {
		if directoryContains(directory, abs) {
			return canonical, true, nil
		}
	}
	allowed, err := allowProject(canonical)
	return canonical, allowed, err
}

func projectInstructionChain(cwd string, roots []string) []string {
	boundary := cwd
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			root = filepath.Join(cwd, root)
		}
		root = filepath.Clean(root)
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		root = canonical
		if directoryContains(root, boundary) {
			boundary = root
		}
	}
	var chain []string
	for directory := cwd; ; directory = filepath.Dir(directory) {
		chain = append(chain, directory)
		if directory == boundary {
			break
		}
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain
}

func directoryContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func promptSkillDirs(chain []string) ([]string, error) {
	var dirs []string
	for _, c := range slices.Backward(chain) {
		dirs = append(dirs, filepath.Join(c, ".agents", "skills"))
	}
	configDirectory, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("user skill directory: %w", err)
	}
	configDirectory, err = filepath.Abs(configDirectory)
	if err != nil {
		return nil, fmt.Errorf("user skill directory: %w", err)
	}
	dirs = append(dirs, filepath.Join(configDirectory, "skills"))
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if !seen[dir] {
			result = append(result, dir)
			seen[dir] = true
		}
	}
	return result, nil
}

func skillPromptScope(path string, chain []string) string {
	for _, directory := range chain {
		if directoryContains(filepath.Join(directory, ".agents", "skills"), path) {
			return directory
		}
	}
	return "all"
}

func readProjectInstructions(path, boundary string) (string, error) {
	root, err := os.OpenRoot(boundary)
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	defer func() { _ = root.Close() }()
	relative, err := filepath.Rel(boundary, path)
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	_, err = root.Lstat(relative)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	info, err := root.Stat(relative)
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("project instructions %s: expected a regular file", path)
	}
	f, err := root.Open(relative)
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProjectInstructionBytes+1))
	if err != nil {
		return "", fmt.Errorf("project instructions %s: %w", path, err)
	}
	if len(data) > maxProjectInstructionBytes {
		return "", fmt.Errorf("project instructions %s exceeds %d bytes", path, maxProjectInstructionBytes)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("project instructions %s is not valid UTF-8", path)
	}
	return string(data), nil
}
