package rpc_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestExecutionDefaultsRPCSharedCASAndSafeProjection(t *testing.T) {
	r, first := fixture(t)
	second, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.HostConfiguration().Update(t.Context(), before.Revision, func(h *config.Host) error {
		h.Providers["fixture"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialEnv: "PRIVATE_TOKEN"}
		h.Defaults.Model = session.ModelSelection{Provider: "fixture", Name: "chat"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read := call[protocol.HostExecutionDefaults](t, first, "host.execution_defaults", protocol.EmptyParams{})
	values := read.ExecutionDefaults
	values.Engine, values.Effort, values.CompactionPercent, values.GoalMaxContinuations, values.MaxAttempts = "quickjs", "high", 75, 9007199254740993, 2
	written := call[protocol.HostExecutionDefaults](t, first, "host.set_execution_defaults", protocol.SetExecutionDefaultsParams{ExpectedRevision: read.Revision, Defaults: values})
	if written.ExecutionDefaults != values || written.Revision == read.Revision {
		t.Fatal("RPC changed defaults or lost revision", written)
	}
	if again := call[protocol.HostExecutionDefaults](t, second, "host.execution_defaults", protocol.EmptyParams{}); !reflect.DeepEqual(again, written) {
		t.Fatal("another client did not see persisted defaults", again)
	}
	requireHistoryError(t, second, "host.set_execution_defaults", protocol.SetExecutionDefaultsParams{ExpectedRevision: read.Revision, Defaults: values}, "CONFLICT")
	// The revision is shared with other host settings, not a second defaults cache.
	call[protocol.HostProfiles](t, second, "host.set_profiles", protocol.SetHostProfilesParams{ExpectedRevision: written.Revision, Profiles: []protocol.HostProfile{{ID: "remote", Name: "Remote", URL: "https://example.test", RuntimeID: "remote_runtime"}}})
	requireHistoryError(t, first, "host.set_execution_defaults", protocol.SetExecutionDefaultsParams{ExpectedRevision: written.Revision, Defaults: values}, "CONFLICT")
	raw, err := json.Marshal(written)
	if err != nil || strings.Contains(string(raw), "PRIVATE_TOKEN") || strings.Contains(string(raw), "credential") || strings.Contains(string(raw), "https://") {
		t.Fatal("defaults projection exposed private route data", err)
	}
}
