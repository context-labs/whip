package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNamedChildTemplateContractKeepsIDsAndExactRetry(t *testing.T) {
	_, c := fixture(t)
	root := *create(t, c).Root
	definition := call[protocol.Definition](t, c, "definitions.register", protocol.DefinitionDocument{ID: "reviewer", Name: "Reviewer", Defaults: protocol.ConfigPatch{Instructions: &protocol.Instructions{Text: "Review carefully"}}})
	root = call[protocol.Session](t, c, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Patch: protocol.ConfigPatch{Children: map[string]protocol.DefinitionRef{"review": definition.Ref}}})
	params := protocol.SpawnSessionParams{Name: "Repository reviewer", Template: "review", ParentID: root.ID, Identity: protocol.RequestIdentity{ClientID: "names", RequestID: "spawn"}, Parts: []protocol.Part{{Type: "text", Text: "Review"}}}
	spawned := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", params)
	retried := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", params)
	if spawned.Session == nil || retried.Session == nil || spawned.Session.Name != params.Name || retried.Session.ID != spawned.Session.ID || retried.Session.Name != params.Name || spawned.Session.Definition != definition.Ref || root.Name != "" {
		t.Fatalf("named child projection or retry: %+v %+v", spawned, retried)
	}
	loaded := call[protocol.Session](t, c, "sessions.get", protocol.SessionParams{SessionID: spawned.Session.ID})
	if loaded.Name != params.Name || loaded.Configuration.Instructions.Text != "Review carefully" {
		t.Fatal("child read lost name or resolved template")
	}
	changed := params
	changed.Name = "Different name"
	requireHistoryError(t, c, "sessions.spawn", changed, "CONFLICT")
	changed.Identity.RequestID = "both"
	changed.Definition = &definition.Ref
	requireHistoryError(t, c, "sessions.spawn", changed, "INVALID")
}
