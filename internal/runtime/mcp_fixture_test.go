package runtime

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/model"
)

func TestMCPHTTPFixtureJoinsRetainedServerSessions(t *testing.T) {
	var server *sdkmcp.Server
	t.Run("owner", func(inner *testing.T) {
		isolateMCP(inner)
		var url string
		url, server, _ = mcpHTTPFixture(inner)
		r, owner, _ := modelHelperFixture(inner, model.Scripted{})
		configureMCPFixture(inner, r, url)
		if _, err := r.MCPRefresh(inner.Context(), owner.ID); err != nil {
			inner.Fatal(err)
		}
		awaitMCPReady(inner, r, owner)
		count := 0
		for connection := range server.Sessions() {
			count++
			// Keep the red regression safe: the outer cleanup owns any session
			// that survives the fixture's HTTP listener and runtime shutdown.
			t.Cleanup(func() { _ = connection.Close() })
		}
		if count == 0 {
			inner.Fatal("expected a connected fixture session")
		}
	})
	if server == nil {
		t.Fatal("fixture did not start")
	}
	for range server.Sessions() {
		t.Fatal("HTTP fixture retained an MCP session after its owner exited")
	}
}
