package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostmodel "github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeContextAuditRetainsAppliedSourceEvidenceWithoutRescanning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "published.md")
	original := "# private comment\nKeep the original instruction.\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	m, provider := nativeUIFixtureStanding(t, path)
	provider.requests = make(chan hostmodel.Request, 4)
	policy := m.owner.Configuration.Instructions
	policy.StandingInstructions = true
	m.owner = nativeMenuRPC[protocol.Session](t, m.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: m.owner.ID, ExpectedRevision: m.owner.ConfigRevision, Patch: protocol.ConfigPatch{Instructions: &policy}})
	nativeMenuRPC[protocol.Grant](t, m.connection, "grants.create", protocol.CreateGrantParams{ID: "audit-standing", SessionID: m.owner.ID, Capability: "instructions.read", Resource: "standing"})
	before := nativeUIControl(t, m, "/context-doctor")
	if before.err != nil || !strings.Contains(m.notice, "No captured attempt") || strings.Contains(m.notice, "published.md") {
		t.Fatal("fresh owner claimed unapplied sources", before.err, m.notice)
	}
	input := nativeUISubmit(t, m, "capture source")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Different text that has never been applied."), 0o600); err != nil {
		t.Fatal(err)
	}
	result := nativeUIControl(t, m, "/context-doctor")
	digest := sha256.Sum256([]byte(original))
	for _, want := range []string{"Applied instruction base:", "bytes/4 heuristic only", "standing_instructions · host root standing · published.md", fmt.Sprintf("%d bytes", len(original)), hex.EncodeToString(digest[:]), "not additive", "does not prepare a provider request"} {
		if result.err != nil || !strings.Contains(m.notice, want) {
			t.Fatal("missing applied source evidence", want, result.err, m.notice)
		}
	}
	if strings.Contains(m.notice, filepath.Dir(path)) || strings.Contains(m.notice, "Different text") || strings.Contains(m.notice, "TOTAL visible context") {
		t.Fatal("audit rescanned, leaked private path or overstated occupancy", m.notice)
	}
	if len(provider.requests) != 1 {
		t.Fatal("audit prepared another model request", len(provider.requests))
	}
	if inputs, err := m.handle.Inputs(ctx, "all", nil, 100); err != nil || len(inputs.Items) != 1 {
		t.Fatal("audit admitted extra work", inputs, err)
	}
}

func TestNativeInstructionAuditKeepsSourceEvidenceBoundedAndNonadditive(t *testing.T) {
	manifest := &protocol.InstructionManifest{Bytes: 9, SHA256: strings.Repeat("b", 64)}
	for range 1152 {
		manifest.Sources = append(manifest.Sources, protocol.InstructionSource{Kind: "project_file", Scope: "workspace", Path: strings.Repeat("界", 100), Bytes: 1000, SHA256: strings.Repeat("a", 64)})
	}
	text := nativeInstructionAudit(manifest)
	if len(text) > nativeNoticeLimit+100 || !strings.Contains(text, "9 bytes (~3 tokens") || !strings.Contains(text, "Display truncated") || strings.Contains(text, "TOTAL") {
		t.Fatal("unbounded or additive source audit", len(text), text[:min(len(text), 300)])
	}
}
