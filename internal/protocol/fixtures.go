package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

type Fixture struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	Valid bool            `json:"valid"`
}

// Fixtures encode actual Go DTOs, including projections from domain values.
// They are deterministic contract evidence, not a substitute for runtime tests.
func Fixtures() ([]Fixture, error) {
	created := time.Date(2026, 9, 27, 12, 0, 0, 123456000, time.UTC)
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		return nil, err
	}
	config, err := session.Resolve(session.Configuration{Model: session.ModelSelection{Provider: "fixture", Name: "scripted"}}, session.Builtins()[0], session.ConfigPatch{})
	if err != nil {
		return nil, err
	}
	root, err := SessionFromDomain(session.Session{ID: "session_root", TreeID: "tree_fixture", Definition: ref, ConfigRevision: 9007199254740993, Config: config, WorkingDirectory: "/workspace", Lifecycle: session.Active, CreatedAt: created})
	if err != nil {
		return nil, err
	}
	child := root
	child.ID = "session_child"
	parent := root.ID
	child.ParentID = &parent
	message := MessageFromDomain(session.Message{ID: "message_fixture", SessionID: "session_child", TurnID: "turn_fixture", Sequence: 9007199254740993, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "Completed."}, {Type: "content", ReferenceID: "content_fixture"}}, CreatedAt: created})
	attempt := ModelAttemptFromDomain(session.ModelAttempt{
		ID: "attempt_fixture", TurnID: "turn_fixture", LogicalID: "call_fixture", Number: 1, State: session.AttemptSucceeded,
		Request: session.ModelRequestSnapshot{Purpose: "turn", Model: session.ModelSelection{Provider: "fixture", Name: "model"}, Route: "https://provider.example/v1/chat/completions", Adapter: "openai-chat", RequestDigest: ref.Revision, MaxOutputTokens: 4096, TimeoutMillis: 30000},
		Result:  &session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(9007199254740993))}, CostNanoUSD: new(int64(9007199254740993)), CostSource: "provider", MessageID: new(session.MessageID(message.ID)), CreatedAt: created, DispatchedAt: &created, FinishedAt: &created,
	})
	contentDigest := sha256.Sum256([]byte("hello"))
	values := []struct {
		name  string
		value any
	}{
		{"PutContentParams", PutContentParams{SessionID: child.ID, ReferenceID: "content_fixture", MediaType: "text/plain", DataBase64: "aGVsbG8="}},
		{"ReadContentResult", ReadContentResult{
			Reference:  ContentReferenceFromDomain(session.ContentReference{ID: "content_fixture", SessionID: session.SessionID(child.ID), Digest: hex.EncodeToString(contentDigest[:]), Size: 5, MediaType: "text/plain", CreatedAt: created}),
			DataBase64: "aGVsbG8=",
		}},
		{"ModelAttemptsResult", ModelAttemptsResult{Items: []ModelAttempt{attempt}}},
		{"InitializeParams", InitializeParams{Major: Major, ExpectedRuntimeID: new(ID("runtime_fixture"))}},
		{"InitializeResult", InitializeResult{Major: Major, Minor: Minor, RuntimeID: "runtime_fixture", Builtins: []DefinitionRef{{ID: ID(ref.ID), Revision: ref.Revision}}}},
		{"Request", Request{JSONRPC: "2.0", ID: "call", Method: "initialize", Params: json.RawMessage(`{"major":4}`)}},
		{"Response", Response{JSONRPC: "2.0", ID: "call", Result: json.RawMessage(`{"items":[]}`)}},
		{"Response", Response{JSONRPC: "2.0", ID: "call", Error: &RPCError{Code: -32009, Kind: "CONFLICT", Message: "request conflict"}}},
		{"Session", root},
		{"Session", child},
		{"HistoryResult", HistoryResult{Items: []Message{message}}},
		{"SubmitParams", SubmitParams{Identity: RequestIdentity{ClientID: "client", RequestID: "request"}, SessionID: child.ID, Source: "user", Parts: []Part{{Type: "text", Text: "Run this."}}}},
		{"UpdateConfigurationParams", UpdateConfigurationParams{SessionID: child.ID, ExpectedRevision: 9007199254740993, Patch: ConfigPatch{Tools: map[string]ToolDeclaration{}, Output: &OutputPolicy{}}}},
		{"Turn", Turn{ID: "turn_fixture", SessionID: child.ID, ConfigRevision: 9007199254740993, State: "running", StartedAt: created.Format(time.RFC3339Nano)}},
	}
	result := make([]Fixture, 0, len(values)+8)
	for _, value := range values {
		raw, err := json.Marshal(value.value)
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: value.name, Value: raw, Valid: true})
	}
	for _, raw := range []string{
		`{"session_id":"session_child","after":9007199254740993,"limit":10}`,
		`{"session_id":"session_child","after":"9223372036854775808","limit":10}`,
		`{"session_id":"session_child","after":"01","limit":10}`,
		`{"session_id":"session_child","after":"-1","limit":10}`,
		`{"session_id":"session_child","after":"0","limit":101}`,
		`{"session_id":"session_child","after":"0","limit":10,"unexpected":true}`,
	} {
		result = append(result, Fixture{Type: "HistoryParams", Value: json.RawMessage(raw), Valid: false})
	}
	result = append(result, Fixture{Type: "SubmitParams", Valid: false, Value: json.RawMessage(`{"identity":{"client_id":"client","request_id":"request"},"session_id":"session_child","source":"user","parts":[{"type":"text","text":"x","reference_id":"content"}]}`)})
	for _, raw := range []string{`{"jsonrpc":"2.0","id":"call"}`, `{"jsonrpc":"2.0","id":"call","result":{},"error":{"code":-32009,"kind":"CONFLICT","message":"conflict"}}`} {
		result = append(result, Fixture{Type: "Response", Value: json.RawMessage(raw), Valid: false})
	}
	result = append(result, Fixture{Type: "InitializeParams", Value: json.RawMessage(`{"major":3}`), Valid: false})
	return result, nil
}
