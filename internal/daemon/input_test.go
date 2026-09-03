package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
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

func TestPrepareAuthoredInputUsesCurrentPersistedFileCapability(t *testing.T) {
	workspace := t.TempDir()
	imagePath := filepath.Join(workspace, "shot.png")
	if err := os.WriteFile(imagePath, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if _, _, err := node.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeCapabilityFor(context.Background(), rootID, rootID, authority.Files.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := node.prepareAuthoredInput(context.Background(), "inspect @shot.png", nil); err == nil || !strings.Contains(err.Error(), "file capability") {
		t.Fatalf("revoked file capability error=%v", err)
	}
}

func TestPrepareAuthoredInputNeverEscapesWorkspace(t *testing.T) {
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
	text, _, err := session.prepareAuthoredInput(context.Background(), "inspect @../secret.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "secret payload") || strings.Contains(text, "the user tagged") {
		t.Fatalf("outside-workspace file was expanded: %q", text)
	}
}
