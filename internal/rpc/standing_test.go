package rpc_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rpc"
	"github.com/context-labs/whip/internal/runtime"
)

func TestHostStandingRPCAbsentPublicationNeverSeedsAFile(t *testing.T) {
	_, c := fixture(t)
	value := call[protocol.HostStandingInstructions](t, c, "host.standing.read", protocol.EmptyParams{})
	if value.Published || value.Revision != nil || value.Text != nil {
		t.Fatal(value)
	}
	requireHistoryError(t, c, "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: strings.Repeat("a", 64), Text: "new source"}, "NOT_FOUND")
}

func TestHostStandingRPCExactCASAndRawPrivateProjection(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "standing-rpc-") //nolint:usetesting // Disposable Unix sockets need short paths on macOS.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(t.TempDir(), "host-private.md")
	if err := os.WriteFile(path, []byte("# raw\r\n rule \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host, err := config.Initialize(directory)
	if err != nil {
		t.Fatal(err)
	}
	host.StandingInstructionsFile = path
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	r, err := runtime.Open(t.Context(), directory, model.Scripted{}, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	server, err := rpc.Listen(r, rpc.HostServices{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	first, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Connect(t.Context(), r.SocketPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	before := call[protocol.HostStandingInstructions](t, first, "host.standing.read", protocol.EmptyParams{})
	if !before.Published || before.Text == nil || *before.Text != "# raw\r\n rule \n" || before.Revision == nil {
		t.Fatal(before)
	}
	written := call[protocol.HostStandingInstructions](t, first, "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *before.Revision, Text: "# preserved\r\n café\n"})
	if *written.Revision == *before.Revision || *written.Text != "# preserved\r\n café\n" {
		t.Fatal(written)
	}
	requireHistoryError(t, second, "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *before.Revision, Text: "stale"}, "CONFLICT")
	read := call[protocol.HostStandingInstructions](t, second, "host.standing.read", protocol.EmptyParams{})
	if *read.Revision != *written.Revision || *read.Text != *written.Text {
		t.Fatal(read, written)
	}
	var invalid protocol.HostStandingInstructions
	if err := first.Call(t.Context(), "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *read.Revision, Text: "nul\x00"}, &invalid); err == nil {
		t.Fatal("invalid text passed client validation")
	}
	requireHistoryError(t, first, "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *read.Revision, Text: strings.Repeat("🙂", 16385)}, "INVALID")
	wire, err := json.Marshal(read)
	if err != nil || strings.Contains(string(wire), filepath.Dir(path)) || strings.Contains(string(wire), "host-private") {
		t.Fatal("host path leaked", string(wire), err)
	}
	cleared := call[protocol.HostStandingInstructions](t, first, "host.standing.write", protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *read.Revision, Text: ""})
	if !cleared.Published || cleared.Text == nil || *cleared.Text != "" {
		t.Fatal(cleared)
	}
}
