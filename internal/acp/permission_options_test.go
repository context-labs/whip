package acp

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
)

// This is editor-adapter coverage. It does not invent a runtime producer of
// child permission requests or weaken the store's delegated-authority rule.
func TestPermissionOptionsMatchRootAndChildAuthority(t *testing.T) {
	root := permissionOptions(protocol.Session{ID: "root"})
	if len(root) != 3 || root[0].OptionId != optAllowOnce || root[1].OptionId != optReject || root[2].OptionId != optAllowAlways {
		t.Fatal("root editor lost concrete approval choices", root)
	}
	child := permissionOptions(protocol.Session{ID: "child", ParentID: new(protocol.ID("root"))})
	if len(child) != 1 || child[0].OptionId != optReject || child[0].Kind != acp.PermissionOptionKindRejectOnce {
		t.Fatal("child editor offered an approval the host rejects", child)
	}
}
