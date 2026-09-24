package daemon

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestSkillCatalogCompletionLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	cwd := canonicalPromptDirectory(t, t.TempDir())
	for i := range 1024 {
		name := fmt.Sprintf("skill-%04d", i)
		writeCompletionSkill(t, cwd, name, name, "metadata", "")
	}
	root := storeBackedInputSession(t, cwd)
	server := &Server{daemon: &Daemon{store: root.root.store}}
	for _, host := range []bool{false, true} {
		complete := func(prefix string, limit int) (CompletionResult, error) {
			if host {
				return server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, Prefix: prefix, Limit: limit})
			}
			return server.completeWorkspace(t.Context(), CompletionParams{RootID: root.id, Kind: "skill", Prefix: prefix, Limit: limit})
		}
		for _, limit := range []int{64, 65, 1024} {
			result, err := complete("", limit)
			if err != nil || len(result.Candidates) != limit || result.Truncated != (limit < 1024) {
				t.Fatalf("host=%v limit=%d: count=%d truncated=%v err=%v", host, limit, len(result.Candidates), result.Truncated, err)
			}
			if result.Candidates[limit-1].Text != fmt.Sprintf("$skill-%04d", limit-1) {
				t.Fatalf("host=%v: catalog order changed", host)
			}
		}
		result, err := complete("skill-1023", 32)
		if err != nil || len(result.Candidates) != 1 || result.Truncated || result.Candidates[0].Text != "$skill-1023" {
			t.Fatalf("host=%v: prefix beyond 64: %+v %v", host, result, err)
		}
		for _, limit := range []int{-1, 0, 1025} {
			if _, err := complete("", limit); err == nil {
				t.Fatalf("host=%v accepted limit=%d", host, limit)
			}
		}
	}
	for _, kind := range []string{"mention", "path"} {
		for _, limit := range []int{64, 65, 1024} {
			_, err := server.completeWorkspace(t.Context(), CompletionParams{RootID: root.id, Kind: kind, Prefix: "missing", Limit: limit})
			if (err != nil) != (limit > 64) {
				t.Fatalf("kind=%s limit=%d: %v", kind, limit, err)
			}
		}
	}
	writeCompletionSkill(t, cwd, "overflow", "overflow", "metadata", "")
	if _, err := server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, Limit: 1024}); err == nil {
		t.Fatal("discovery ceiling silently returned a partial catalog")
	}
	if _, err := server.completeWorkspace(t.Context(), CompletionParams{RootID: root.id, Kind: "skill", Limit: 1024}); err == nil {
		t.Fatal("workspace discovery ceiling silently returned a partial catalog")
	}
}

func TestSkillCatalogCompletionByteOverflow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	cwd := canonicalPromptDirectory(t, t.TempDir())
	for i := range 1024 {
		// Warned names are still invocable. HTML escaping expands their JSON bytes.
		name := strings.Repeat("<", 200) + fmt.Sprintf("%04d", i)
		writeCompletionSkill(t, cwd, fmt.Sprintf("entry-%04d", i), name, `"`+strings.Repeat("&", 80)+`"`, "")
	}
	root := storeBackedInputSession(t, cwd)
	server := &Server{daemon: &Daemon{store: root.root.store}}
	host, err := server.completeHostSkills(t.Context(), protocol.HostSkillCompletionParams{CWD: cwd, Limit: 1024})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := server.completeWorkspace(t.Context(), CompletionParams{RootID: root.id, Kind: "skill", Limit: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []CompletionResult{host, workspace} {
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > maxSkillCompletionBytes || !result.Truncated || len(result.Candidates) >= 1024 || len(result.Candidates) < 64 || len(result.Warnings) != 16 {
			t.Fatalf("bytes=%d candidates=%d warnings=%d truncated=%v err=%v", len(encoded), len(result.Candidates), len(result.Warnings), result.Truncated, err)
		}
		if strings.Contains(string(encoded), cwd) || strings.Contains(string(encoded), "SECRET_BODY_NOT_METADATA") {
			t.Fatal("completion exposed a path or skill body")
		}
	}
}

func TestSkillCatalogCompletionSerializedBudget(t *testing.T) {
	for _, alreadyTruncated := range []bool{false, true} {
		input := CompletionResult{Truncated: alreadyTruncated}
		for range 16 {
			input.Warnings = append(input.Warnings, strings.Repeat("<", 512))
		}
		for i := range 1024 {
			input.Candidates = append(input.Candidates, protocol.CompletionCandidate{
				Text:        "$" + strings.Repeat("<", 250) + fmt.Sprintf("%04d", i),
				Description: strings.Repeat("&", 80),
			})
		}
		encoded, err := json.Marshal(input)
		if err != nil || len(encoded) <= maxSkillCompletionBytes {
			t.Fatalf("fixture must overflow serialized bytes: %d %v", len(encoded), err)
		}
		result, err := boundSkillCompletion(input)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err = json.Marshal(result)
		if err != nil || len(encoded) > maxSkillCompletionBytes || !result.Truncated || len(result.Candidates) == 0 {
			t.Fatalf("unbounded response: bytes=%d count=%d truncated=%v err=%v", len(encoded), len(result.Candidates), result.Truncated, err)
		}
		if !reflect.DeepEqual(result.Warnings, input.Warnings) || !reflect.DeepEqual(result.Candidates, input.Candidates[:len(result.Candidates)]) {
			t.Fatal("budget changed warnings or candidate order")
		}
		result.Candidates = input.Candidates[:len(result.Candidates)+1]
		encoded, err = json.Marshal(result)
		if err != nil || len(encoded) <= maxSkillCompletionBytes {
			t.Fatalf("budget discarded a candidate that fit: bytes=%d err=%v", len(encoded), err)
		}
	}
	input := CompletionResult{Candidates: []protocol.CompletionCandidate{{Text: "$café", Description: "多字节"}}}
	result, err := boundSkillCompletion(input)
	if err != nil || !reflect.DeepEqual(result, input) {
		t.Fatalf("complete metadata changed: %+v %v", result, err)
	}
}

func TestSkillCatalogCompletionExactByteBoundary(t *testing.T) {
	for _, overflow := range []int{0, 1} {
		input := CompletionResult{Candidates: []protocol.CompletionCandidate{{Text: "$name"}}}
		baseline, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		// A synthetic large field isolates the exact JSON envelope boundary.
		input.Candidates[0].Description = strings.Repeat("x", maxSkillCompletionBytes-len(baseline)+overflow)
		result, err := boundSkillCompletion(input)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil || len(encoded) > maxSkillCompletionBytes || result.Truncated != (overflow != 0) || len(result.Candidates) != 1-overflow {
			t.Fatalf("overflow=%d bytes=%d candidates=%d truncated=%v err=%v", overflow, len(encoded), len(result.Candidates), result.Truncated, err)
		}
	}
}

func TestSkillCatalogCompletionCapabilityNegotiation(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	for _, requested := range [][]string{nil, {"workspace_completion", "host_skill_completion", "host_global_skill_completion", "skill_catalog_completion", "unknown"}} {
		client, err := DialWebSocketClient(t.Context(), fixture.endpoint, InitializeParams{
			ProtocolMajor: ProtocolMajor, ClientKind: "human", ClientID: "catalog-capability", Capabilities: requested,
		})
		if err != nil {
			t.Fatal(err)
		}
		initial := client.InitializeResult()
		_ = client.Close()
		for _, capability := range []string{"workspace_completion", "host_skill_completion", "host_global_skill_completion", "skill_catalog_completion"} {
			if !slices.Contains(initial.Capabilities, capability) {
				t.Fatalf("missing supported capability %s", capability)
			}
			if slices.Contains(initial.NegotiatedCapabilities, capability) != slices.Contains(requested, capability) {
				t.Fatalf("unexpected negotiation for %s: %v", capability, initial.NegotiatedCapabilities)
			}
		}
		if slices.Contains(initial.NegotiatedCapabilities, "unknown") {
			t.Fatal("negotiated unsupported capability")
		}
	}
}
