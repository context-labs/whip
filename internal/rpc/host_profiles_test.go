package rpc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestHostProfilesRPCSharedCASNoConnectionsOrCredentialProjection(t *testing.T) {
	r, first := fixture(t)
	second, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(endpoint.Close)
	before, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	privateRoute := config.Provider{
		Kind: "openai-chat", BaseURL: endpoint.URL + "/v1", CredentialSource: "command",
		CredentialCommand: &config.CredentialCommand{Executable: "/usr/bin/false", Arguments: []string{"private-test-secret"}},
	}
	_, err = r.HostConfiguration().Update(t.Context(), before.Revision, func(host *config.Host) error {
		host.Providers["private-route"] = privateRoute
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read := call[protocol.HostProfiles](t, first, "host.profiles", protocol.EmptyParams{})
	if read.Profiles == nil || len(read.Profiles) != 0 {
		t.Fatal("missing profiles must be an explicit empty list", read)
	}
	profiles := []protocol.HostProfile{{ID: "remote", Name: " Remote ", URL: endpoint.URL + "/", RuntimeID: "pinned-runtime", ConnectOnLaunch: true}}
	written := call[protocol.HostProfiles](t, first, "host.set_profiles", protocol.SetHostProfilesParams{
		ExpectedRevision: read.Revision, Profiles: profiles,
	})
	if written.Profiles[0].URL != profiles[0].URL || written.Profiles[0].Name != "Remote" || written.Revision == read.Revision {
		t.Fatal("profile update lost exact URL, display name, or revision", written)
	}
	if again := call[protocol.HostProfiles](t, second, "host.profiles", protocol.EmptyParams{}); !reflect.DeepEqual(again, written) {
		t.Fatal("another client did not see persisted profiles", again)
	}
	same := call[protocol.HostProfiles](t, first, "host.set_profiles", protocol.SetHostProfilesParams{
		ExpectedRevision: written.Revision, Profiles: written.Profiles,
	})
	if !reflect.DeepEqual(same, written) {
		t.Fatal("same profile list changed revision")
	}
	requireHistoryError(t, second, "host.set_profiles", protocol.SetHostProfilesParams{
		ExpectedRevision: read.Revision, Profiles: written.Profiles,
	}, "CONFLICT")
	encoded, err := json.Marshal(written)
	if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "credential") {
		t.Fatal("safe host projection exposed private provider configuration", err)
	}
	loaded, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(loaded.Host.Providers["private-route"], privateRoute) {
		t.Fatal("profiles changed provider configuration", err)
	}
	cleared := call[protocol.HostProfiles](t, second, "host.set_profiles", protocol.SetHostProfilesParams{
		ExpectedRevision: written.Revision, Profiles: []protocol.HostProfile{},
	})
	if cleared.Profiles == nil || len(cleared.Profiles) != 0 || cleared.Revision == written.Revision || requests.Load() != 0 {
		t.Fatal("profiles connected, failed to clear, or lost revision", cleared, requests.Load())
	}
	bad := profiles[0]
	bad.URL = endpoint.URL + ":65536"
	requireHistoryError(t, first, "host.set_profiles", protocol.SetHostProfilesParams{
		ExpectedRevision: cleared.Revision, Profiles: []protocol.HostProfile{bad},
	}, "INVALID")
}
