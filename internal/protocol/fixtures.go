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
	callMessage := MessageFromDomain(session.Message{
		ID: "message_call", SessionID: "session_child", TurnID: "turn_fixture", Sequence: 9007199254740994,
		Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call_fixture", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}, CreatedAt: created,
	})
	toolMessage := MessageFromDomain(session.Message{
		ID: "message_result", SessionID: "session_child", TurnID: "turn_fixture", Sequence: 9007199254740995,
		Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "call_fixture", Output: "1", IsError: false}}}, CreatedAt: created,
	})
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
		{"MailAdmission", MailAdmission{MailID: "mail_deleted", DeletedAt: new(created.Format(time.RFC3339Nano))}},
		{"ReadMailResult", ReadMailResult{Mail: MailMetadataFromDomain(session.MailMetadata{ID: "mail_fixture", Revision: 128, Source: session.MailSource{Kind: "session", ID: "session_root"}, RecipientID: "session_child", Delivery: session.MailNextTurn, Subject: "Subject", BodyBytes: 5, State: session.MailPending, AvailableAt: created, CreatedAt: created, RevisedAt: created}), Body: "hello"}},
		{"Message", MessageFromDomain(session.Message{ID: "message_mail", SessionID: "session_child", TurnID: "turn_fixture", Sequence: 9007199254740995, Role: session.User, Mail: &session.MailRef{ID: "mail_fixture", Revision: 128, Presentation: session.MailDigest}, Parts: []session.Part{{Type: "text", Text: "Mail from session_root: Subject"}}, CreatedAt: created})},
		{"Budget", BudgetFromDomain(session.Budget{SessionID: "session_root", Kind: session.BudgetModelTokens, Revision: 9007199254740993, Limit: new(int64(9007199254740994)), Used: 9007199254740993, Reserved: 1})},
		{"SessionObservation", SessionObservation{Epoch: "boot_fixture", Messages: []Message{message}, Preview: &MessagePreview{AttemptID: "attempt_live", TurnID: "turn_fixture", MessageID: "message_live", Revision: 9007199254740993, Text: "In progress", Calls: []CallPreview{{Index: 0, ID: "call_partial", Name: "execute", Arguments: `{"code":"print(`}}}}},
		{"SessionObservation", SessionObservation{Epoch: "boot_restarted", Messages: []Message{}, Preview: nil}},
		{"Grant", GrantFromDomain(session.Grant{ID: "grant_fixture", SessionID: "session_child", Capability: "files.read", Resource: "/workspace", IssuerID: new(session.GrantID("grant_parent")), CreatedAt: created})},
		{"HostOperation", OperationFromDomain(session.Operation{ID: "operation_fixture", CellID: "cell_fixture", RequestID: "1:1", Capability: "files.read", Resource: "/workspace", Arguments: json.RawMessage(`{"path":"example.txt","offset":1,"limit":2000}`), SessionID: "session_child", TurnID: "turn_fixture", State: session.OperationSucceeded, GrantID: new(session.GrantID("grant_fixture")), Result: &session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{"output":"1: hello"}`)}, CreatedAt: created, DispatchedAt: &created, FinishedAt: &created})},
		{"Permission", PermissionFromDomain(session.Permission{OperationID: "operation_fixture", State: session.PermissionApproved, CreatedAt: created, ResolvedAt: &created})},
		{"Cell", CellFromDomain(session.Cell{ID: "cell_fixture", SessionID: "session_child", TurnID: "turn_fixture", CallMessageID: "message_call", CallID: "call_fixture", State: session.CellSucceeded, ResultMessageID: new(session.MessageID("message_result")), Checkpoint: &session.Checkpoint{Digest: ref.Revision, Size: 123, Engine: session.Starlark, Metadata: json.RawMessage(`{"format_version":1}`)}, CreatedAt: created, FinishedAt: &created})},
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
		{"HistoryResult", HistoryResult{Items: []Message{message, callMessage, toolMessage}}},
		{"Part", callMessage.Parts[0]},
		{"Part", toolMessage.Parts[0]},
		{"SpawnSessionParams", SpawnSessionParams{Identity: RequestIdentity{ClientID: "client", RequestID: "spawn"}, ParentID: root.ID, Parts: []Part{{Type: "text", Text: "Child work"}}, GrantIDs: []ID{}}},
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
	result = append(result, Fixture{Type: "SubmitParams", Valid: false, Value: json.RawMessage(`{"identity":{"client_id":"client","request_id":"request"},"session_id":"session_child","source":"user","parts":[{"type":"tool_call","call":{"id":"call_fixture","name":"execute","arguments":{"code":"print(1)"}}}]}`)})
	for _, raw := range []string{
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":null}}`,
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":[]}}`,
		`{"type":"tool_call","call":{"id":"call","name":"execute","arguments":{}},"text":"extra"}`,
		`{"type":"tool_result","result":{"call_id":"call","output":"missing flag"}}`,
	} {
		result = append(result, Fixture{Type: "Part", Valid: false, Value: json.RawMessage(raw)})
	}
	callMessage.Role = "user"
	toolMessage.Role = "assistant"
	for _, message := range []Message{callMessage, toolMessage} {
		raw, err := json.Marshal(HistoryResult{Items: []Message{message}})
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "HistoryResult", Value: raw, Valid: false})
	}
	for _, raw := range []string{`{"jsonrpc":"2.0","id":"call"}`, `{"jsonrpc":"2.0","id":"call","result":{},"error":{"code":-32009,"kind":"CONFLICT","message":"conflict"}}`} {
		result = append(result, Fixture{Type: "Response", Value: json.RawMessage(raw), Valid: false})
	}
	result = append(result, Fixture{Type: "InitializeParams", Value: json.RawMessage(`{"major":3}`), Valid: false})
	return result, nil
}
