package rpc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestShellHumanControlsValidateAndDoNotCreateResources(t *testing.T) {
	_, c := fixture(t)
	root := create(t, c).Root.ID
	view := call[protocol.ShellInteractionResult](t, c, "shell.interaction", protocol.ShellInteractionParams{SessionID: root, Cursor: 0})
	if view.Interaction != nil {
		t.Fatal(view)
	}
	for _, input := range []struct {
		data string
		kind string
	}{{"!invalid-base64", "INVALID"}, {strings.Repeat("A", 22000), "INVALID"}, {"eA==", "NOT_FOUND"}} {
		var result protocol.ShellInputResult
		var remote *client.Error
		err := c.Call(t.Context(), "shell.input", protocol.ShellInputParams{SessionID: root, OperationID: "nonexistent", Sequence: 1, DataBase64: input.data}, &result)
		if !errors.As(err, &remote) || remote.Kind != input.kind {
			t.Fatal(input.kind, err)
		}
	}
}
