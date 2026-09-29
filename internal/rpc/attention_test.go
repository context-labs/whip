package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestHostAttentionWirePagesExactOwnerCounts(t *testing.T) {
	_, c := fixture(t)
	root := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: "attention", Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "fixture", Name: "model"}}}).Root
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "human", RequestID: "queued"}, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "queued"}}})
	page := call[protocol.HostAttentionResult](t, c, "host.attention", protocol.HostAttentionParams{Limit: 10, MaxBytes: 4096})
	if len(page.Items) != 1 || page.Items[0].Activity.QueuedInputCount != 1 || page.Items[0].SessionID != root.ID || page.Items[0].RootID != root.ID {
		t.Fatal(page)
	}
}
