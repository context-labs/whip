package daemon

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func inputTestSession(t *testing.T, workspace string) *AgentSession {
	t.Helper()
	value := agent.NewRuntime(llm.New("http://unused.invalid", ""), "model", 100, "system", tools.NewServices())
	value.WorkingDir = workspace
	return &AgentSession{agent: value}
}

func TestPrepareAuthoredInputExpandsSkillsAndWorkspaceMentions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.go"), []byte("package notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(workspace, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: inspect changes\n---\nReview carefully.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := inputTestSession(t, workspace)
	text, parts, err := session.prepareAuthoredInput(context.Background(), "use @notes.go#1 and $review", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 || !strings.Contains(text, filepath.Join(workspace, "notes.go")+" (lines 1)") ||
		!strings.Contains(text, `<invoked_skill name="review"`) || !strings.Contains(text, "Review carefully.") {
		t.Fatalf("expanded input=%q parts=%+v", text, parts)
	}
}

func TestPrepareAuthoredInputOverrideSkillFallbackUsesCurrentAuthority(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	workspace := t.TempDir()
	writeDaemonPromptFile(t, filepath.Join(workspace, ".agents", "skills", "broken", "SKILL.md"), "malformed metadata")
	node := storeBackedInputSession(t, workspace)
	node.root.authority = node.authority
	node.promptOverride = "CUSTOM_ROOT_PROMPT"
	if err := node.refreshPrompt(t.Context()); err != nil {
		t.Fatal(err)
	}
	if text, _, err := node.prepareAuthoredInput(t.Context(), "plain input", nil); err != nil || text != "plain input" {
		t.Fatalf("input without skill invocation scanned an unrelated broken catalog: text=%q error=%v", text, err)
	}
	outside := t.TempDir()
	writeDaemonPromptFile(t, filepath.Join(outside, ".agents", "skills", "outside", "SKILL.md"), "malformed metadata")
	writeDaemonPromptFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "skills", "global", "SKILL.md"), "---\nname: global\ndescription: global rule\n---\nGLOBAL_SKILL_BODY")
	node.agent.WorkingDir = outside
	text, _, err := node.prepareAuthoredInput(t.Context(), "$global please", nil)
	if err != nil || !strings.Contains(text, "GLOBAL_SKILL_BODY") {
		t.Fatalf("scoped override fallback lost global skill: text=%q error=%v", text, err)
	}
	if text, _, err := node.prepareAuthoredInput(t.Context(), "$outside please", nil); err != nil || text != "$outside please" {
		t.Fatalf("fallback read an unauthorized project catalog: text=%q error=%v", text, err)
	}
}

func TestPrepareAuthoredInputRechecksCachedProjectSkills(t *testing.T) {
	for _, change := range []string{"mode downgrade", "symlink retarget"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("WHIP_HOME", t.TempDir())
			workspace := t.TempDir()
			outside := t.TempDir()
			node := storeBackedInputSession(t, workspace)
			node.root.authority = node.authority
			skillPath := filepath.Join(workspace, ".agents", "skills", "local", "SKILL.md")
			body := "---\nname: local\ndescription: local rule\n---\nPROJECT_SKILL_BODY"
			if change == "mode downgrade" {
				node.agent.WorkingDir = outside
				skillPath = filepath.Join(outside, ".agents", "skills", "local", "SKILL.md")
				writeDaemonPromptFile(t, skillPath, body)
				if err := node.root.store.SetPermissionMode(t.Context(), node.id, sessionstore.PermissionModeAutomatic); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(workspace, "local.md")
				writeDaemonPromptFile(t, target, body)
				if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, skillPath); err != nil {
					t.Fatal(err)
				}
			}
			if err := node.refreshPrompt(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(node.prompt.Skills) != 1 {
				t.Fatalf("fixture did not cache the project skill: %+v", node.prompt.Skills)
			}
			if change == "mode downgrade" {
				if err := node.root.store.SetPermissionMode(t.Context(), node.id, sessionstore.PermissionModePrompt); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(outside, "retargeted.md")
				writeDaemonPromptFile(t, target, "FORBIDDEN_SKILL_BODY")
				if err := os.Remove(skillPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, skillPath); err != nil {
					t.Fatal(err)
				}
			}
			text, parts, err := node.prepareAuthoredInput(t.Context(), "$local please", nil)
			if !errors.Is(err, capability.ErrDenied) || text != "" || len(parts) != 0 {
				t.Fatalf("cached skill bypassed current authority: text=%q parts=%d error=%v", text, len(parts), err)
			}
		})
	}
}

func TestPrepareAuthoredInputAttachesVisionImagesAndEnforcesChildCapability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(imagePath, []byte("not-decoded-by-expansion"), 0o600); err != nil {
		t.Fatal(err)
	}

	session := inputTestSession(t, workspace)
	session.agent.Vision = true
	text, parts, err := session.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Type != "image_url" || parts[0].ImageURL == nil || !strings.Contains(text, "attached image") {
		t.Fatalf("vision expansion text=%q parts=%+v", text, parts)
	}

	session.parentID = "parent"
	session.capabilities = nil
	if _, _, err := session.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil); err == nil || !strings.Contains(err.Error(), "file capability") {
		t.Fatalf("child mention capability error=%v", err)
	}
}

// storeBackedInputSession binds the node to a real root so the persisted,
// workspace-scoped file grant is exercised rather than the detached shortcut.
func storeBackedInputSession(t *testing.T, workspace string) *AgentSession {
	t.Helper()
	store, err := sessionstore.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(sessionstore.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	node := inputTestSession(t, workspace)
	node.agent.Vision = true
	node.root = &Session{store: store, meta: sessionstore.Meta{ID: rootID}}
	node.id = rootID
	node.authority = authority
	return node
}

func TestPrepareAuthoredInputUsesCurrentPersistedFileCapability(t *testing.T) {
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(imagePath, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	node := storeBackedInputSession(t, workspace)
	if _, _, err := node.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := node.root.store.RevokeCapabilityFor(context.Background(), node.id, node.id, node.authority.Files.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := node.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil); err == nil || !strings.Contains(err.Error(), "file capability") {
		t.Fatalf("revoked file capability error=%v", err)
	}
}

func TestPrepareAuthoredInputDetachedSessionRemainsProjectScoped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := t.TempDir()
	workspace := filepath.Join(parent, "work")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := inputTestSession(t, workspace)
	text, parts, err := session.prepareAuthoredInput(context.Background(), "inspect @../secret.txt", nil)
	if err == nil || !strings.Contains(err.Error(), "file capability") || text != "" || len(parts) != 0 {
		t.Fatalf("detached session accepted outside mention: text=%q parts=%d error=%v", text, len(parts), err)
	}
}

func TestPrepareAuthoredInputOutsideMentionsFollowSavedMode(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "sibling")
	for _, dir := range []string{workspace, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	imagePath := filepath.Join(outside, "shot.png")
	notePath := filepath.Join(outside, "notes.txt")
	insidePath := filepath.Join(workspace, "inside.txt")
	for _, path := range []string{imagePath, notePath, insidePath} {
		if err := os.WriteFile(path, []byte("fixture payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	node := storeBackedInputSession(t, workspace)
	for _, mode := range []struct {
		name  string
		value string
		allow bool
	}{
		{name: "initial Ask", value: sessionstore.PermissionModePrompt},
		{name: "Full Access", value: sessionstore.PermissionModeAutomatic, allow: true},
		{name: "downgraded Ask", value: sessionstore.PermissionModePrompt},
	} {
		t.Run(mode.name, func(t *testing.T) {
			if err := node.root.store.SetPermissionMode(t.Context(), node.id, mode.value); err != nil {
				t.Fatal(err)
			}
			for _, mention := range []struct {
				name   string
				value  string
				path   string
				images int
			}{
				{name: "absolute image", value: imagePath, path: imagePath, images: 1},
				{name: "relative image", value: "../sibling/shot.png", path: imagePath, images: 1},
				{name: "symlink image", value: "alias/shot.png", path: imagePath, images: 1},
				{name: "regular file", value: notePath, path: notePath},
			} {
				t.Run(mention.name, func(t *testing.T) {
					text, parts, err := node.prepareAuthoredInput(t.Context(), "inspect @"+mention.value, nil)
					if !mode.allow {
						if !errors.Is(err, capability.ErrDenied) || text != "" || len(parts) != 0 {
							t.Fatalf("outside mention should be denied: text=%q parts=%d error=%v", text, len(parts), err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					_, note, _ := strings.Cut(text, "[note:")
					if !strings.Contains(note, mention.path) || len(parts) != mention.images {
						t.Fatalf("outside mention was not expanded: text=%q parts=%d", text, len(parts))
					}
				})
			}
		})
	}
	// A downgrade keeps cwd but must allow an absolute authorized target.
	node.agent.WorkingDir = outside
	if _, _, err := node.prepareAuthoredInput(t.Context(), "inspect @"+insidePath, nil); err != nil {
		t.Fatalf("authorized mention from outside cwd: %v", err)
	}
}

func TestPrepareAuthoredInputFullAccessKeepsFuzzyMentionsInProject(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "project")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside-needle.txt")
	if err := os.WriteFile(outside, []byte("external payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "alias-needle.txt")); err != nil {
		t.Fatal(err)
	}
	node := storeBackedInputSession(t, workspace)
	if err := node.root.store.SetPermissionMode(t.Context(), node.id, sessionstore.PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	input := "inspect @needle"
	text, parts, err := node.prepareAuthoredInput(t.Context(), input, nil)
	if err != nil || text != input || len(parts) != 0 {
		t.Fatalf("fuzzy mention searched outside the project: text=%q parts=%d error=%v", text, len(parts), err)
	}
}

// The TUI saves clipboard images under <config dir>/pastes and tags them by
// absolute path. This exception also works in Ask, but still needs a live read
// capability and does not cover other outside images or regular files.
func TestPrepareAuthoredInputAttachesPastedImagesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	pastes := filepath.Join(home, "pastes")
	if err := os.Mkdir(pastes, 0o700); err != nil {
		t.Fatal(err)
	}
	pasted := filepath.Join(pastes, "shot.png")
	elsewhere := filepath.Join(home, "elsewhere.png")
	notes := filepath.Join(pastes, "notes.txt")
	for _, path := range []string{pasted, elsewhere, notes} {
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	node := storeBackedInputSession(t, t.TempDir())
	text, parts, err := node.prepareAuthoredInput(context.Background(), "look at @"+pasted, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, note, _ := strings.Cut(text, "[note:") // the input echoes every token; only the note shows what resolved
	if len(parts) != 1 || !strings.Contains(note, "shot.png (attached image)") ||
		strings.Contains(note, "elsewhere") || strings.Contains(note, "notes.txt") {
		t.Fatalf("paste expansion text=%q parts=%d", text, len(parts))
	}
	for _, path := range []string{elsewhere, notes} {
		if _, _, err := node.prepareAuthoredInput(t.Context(), "inspect @"+path, nil); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("non-paste mention %q error=%v, want scope denial", path, err)
		}
	}
	if _, err := node.root.store.RevokeCapabilityFor(t.Context(), node.id, node.id, node.authority.Files.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := node.prepareAuthoredInput(t.Context(), "inspect @"+pasted, nil); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("pasted image bypassed revoked read capability: %v", err)
	}
}

func TestPrepareAuthoredInputNormalizesOversizedImages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, llm.NormalizeMaxDim+100, 40))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "wide.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	session := inputTestSession(t, workspace)
	session.agent.Vision = true
	_, parts, err := session.prepareAuthoredInput(context.Background(), "inspect @wide.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].W == 0 || parts[0].W > llm.NormalizeMaxDim || !strings.HasPrefix(parts[0].ImageURL.URL, "data:image/jpeg") {
		t.Fatalf("normalized parts=%+v", parts)
	}
}

// macOS screenshot names contain spaces, and a Finder drag escapes them as
// "a\ b.png"; a mention split at whitespace must still find the file.
func TestPrepareAuthoredInputResolvesMentionPathsWithSpaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	shot := filepath.Join(workspace, "Screenshot 2026-09-04 at 10.00.00.png")
	// A sibling screenshot keeps the bare "@Screenshot" fuzzy match ambiguous,
	// so only the space-extended path can resolve.
	for _, path := range []string{shot, filepath.Join(workspace, "Screenshot 2026-09-04 at 11.00.00.png"), filepath.Join(workspace, "a b.png")} {
		if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session := inputTestSession(t, workspace)
	session.agent.Vision = true

	text, parts, err := session.prepareAuthoredInput(context.Background(), "see @Screenshot 2026-09-04 at 10.00.00.png now", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || !strings.Contains(text, shot+" (attached image)") {
		t.Fatalf("space path text=%q parts=%d", text, len(parts))
	}

	text, parts, err = session.prepareAuthoredInput(context.Background(), `see @a\ b.png`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || !strings.Contains(text, filepath.Join(workspace, "a b.png")+" (attached image)") {
		t.Fatalf("escaped path text=%q parts=%d", text, len(parts))
	}
}

// Space-joined extensions resolve as exact paths only: a partial multi-word
// name must not fuzzy-walk the workspace once per extension.
func TestPrepareAuthoredInputMentionExtensionsDoNotFuzzyMatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	// Two screenshots keep the bare "@Screenshot" ambiguous; only a fuzzy
	// walk on "Screenshot 2026" could single out the first.
	for _, name := range []string{"Screenshot 2026 final.png", "Screenshot 2025 final.png"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session := inputTestSession(t, workspace)
	input := "see @Screenshot 2026 rocks"
	text, parts, err := session.prepareAuthoredInput(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != input || len(parts) != 0 {
		t.Fatalf("partial multi-word mention resolved: text=%q parts=%d", text, len(parts))
	}
}

func TestPrepareAuthoredInputLeavesUnresolvedMentionWordsAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	session := inputTestSession(t, t.TempDir())
	input := "@missing file here"
	text, parts, err := session.prepareAuthoredInput(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != input || len(parts) != 0 {
		t.Fatalf("unresolved mention text=%q parts=%d", text, len(parts))
	}
}

// The shortest resolving candidate wins: even when a longer space-joined
// name also exists on disk, a token that resolves on its own leaves the
// following words alone so they can be scanned as mentions themselves.
func TestPrepareAuthoredInputMentionDoesNotSwallowFollowingWords(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, err := filepath.EvalSymlinks(t.TempDir()) // the note names canonical paths
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes.go", "notes.go with", "other.go"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("package x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session := inputTestSession(t, workspace)
	text, _, err := session.prepareAuthoredInput(context.Background(), "compare @notes.go with @other.go please", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, note, _ := strings.Cut(text, "[note:") // the input echo itself contains "notes.go with"
	if !strings.Contains(note, filepath.Join(workspace, "notes.go")+"; "+filepath.Join(workspace, "other.go")) || strings.Contains(note, "notes.go with") {
		t.Fatalf("both mentions should resolve without swallowing words: %q", text)
	}
}
