package rpc_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/terminal"
)

func TestHumanTerminalsAreEphemeralHostResources(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	manager := terminal.NewManager(t.Context())
	defer manager.Shutdown()
	r, c := fixtureHost(t, rpc.HostServices{Terminals: manager})
	epoch := protocol.ID(r.ProcessEpoch())
	opened := call[protocol.TerminalInfo](t, c, "terminal.open", protocol.TerminalOpenParams{ProcessEpoch: epoch, Cwd: t.TempDir(), Cols: 80, Rows: 24})
	ref := protocol.TerminalRef{ProcessEpoch: epoch, ID: opened.ID}
	if opened.ProcessEpoch != epoch || opened.Exited || opened.Shell != "/bin/sh" {
		t.Fatalf("opened=%+v", opened)
	}
	call[protocol.TerminalAccepted](t, c, "terminal.write", protocol.TerminalWriteParams{ProcessEpoch: epoch, ID: opened.ID, DataBase64: base64.StdEncoding.EncodeToString([]byte("stty -echo; printf 'public-%s-end\\n' terminal\n"))})
	cursor := protocol.Counter(0)
	text := ""
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(text, "public-terminal-end") && time.Now().Before(deadline) {
		page := call[protocol.TerminalPage](t, c, "terminal.read", protocol.TerminalReadParams{ProcessEpoch: epoch, ID: opened.ID, Cursor: cursor, Limit: 7})
		bytes, err := base64.StdEncoding.DecodeString(page.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		if len(bytes) > 7 || page.Next != page.From+protocol.Counter(len(bytes)) {
			t.Fatal("invalid byte cursor")
		}
		text += string(bytes)
		cursor = page.Next
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(text, "public-terminal-end") {
		t.Fatal(text)
	}
	resized := call[protocol.TerminalInfo](t, c, "terminal.resize", protocol.TerminalResizeParams{ProcessEpoch: epoch, ID: opened.ID, Cols: 120, Rows: 45})
	if resized.Cols != 120 || resized.Rows != 45 {
		t.Fatal(resized)
	}
	listed := call[protocol.TerminalList](t, c, "terminal.list", protocol.TerminalListParams{ProcessEpoch: epoch})
	if len(listed.Items) != 1 || listed.Items[0].ID != opened.ID {
		t.Fatal(listed)
	}
	for _, method := range []string{"terminal.list", "terminal.open", "terminal.read", "terminal.write", "terminal.resize", "terminal.close"} {
		var params any
		switch method {
		case "terminal.list":
			params = protocol.TerminalListParams{ProcessEpoch: "stale"}
		case "terminal.open":
			params = protocol.TerminalOpenParams{ProcessEpoch: "stale", Cwd: t.TempDir(), Cols: 80, Rows: 24}
		case "terminal.read":
			params = protocol.TerminalReadParams{ProcessEpoch: "stale", ID: opened.ID, Limit: 1}
		case "terminal.write":
			params = protocol.TerminalWriteParams{ProcessEpoch: "stale", ID: opened.ID, DataBase64: "eA=="}
		case "terminal.resize":
			params = protocol.TerminalResizeParams{ProcessEpoch: "stale", ID: opened.ID, Cols: 80, Rows: 24}
		default:
			params = protocol.TerminalRef{ProcessEpoch: "stale", ID: opened.ID}
		}
		var result any
		var remote *client.Error
		err := c.Call(t.Context(), method, params, &result)
		if !errors.As(err, &remote) || remote.Kind != "IDENTITY" {
			t.Fatalf("%s stale epoch=%v", method, err)
		}
	}
	if len(manager.List()) != 1 {
		t.Fatal("stale open started another resource")
	}
	call[protocol.TerminalAccepted](t, c, "terminal.close", ref)
	if items := call[protocol.TerminalList](t, c, "terminal.list", protocol.TerminalListParams{ProcessEpoch: epoch}).Items; len(items) != 0 {
		t.Fatal(items)
	}
	if tree := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 100}); len(tree.Items) != 0 {
		t.Fatal("human terminal created a session")
	}
}
