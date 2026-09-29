package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/protocol"
)

func TestLanguageServerStatusDoesNotStartProcesses(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c)
	snapshot, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.HostConfiguration().Update(t.Context(), snapshot.Revision, func(host *config.Host) error {
		host.LSP = map[string]lsp.Config{"uninstalled": {Command: []string{"does-not-exist"}, Extensions: []string{".custom"}, Env: map[string]string{"PRIVATE": "secret"}}, "gopls": {Enabled: new(false)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result := call[protocol.LanguageServersResult](t, c, "lsp.status", protocol.SessionParams{SessionID: root.Root.ID})
		if len(result.Items) != 1 || result.Items[0].Name != "uninstalled" || result.Items[0].State != "not_started" || result.Items[0].WorkspaceRoot != nil || result.Items[0].Failure != nil {
			t.Fatal(result)
		}
	}
	var result protocol.LanguageServersResult
	if err := c.Call(t.Context(), "lsp.status", protocol.SessionParams{SessionID: "missing"}, &result); !rpcKind(err, "NOT_FOUND") {
		t.Fatal(err)
	}
	if err := c.Call(t.Context(), "lsp.restart", protocol.SessionParams{SessionID: root.Root.ID}, &result); err == nil {
		t.Fatal("unsupported restart exposed")
	}
}
