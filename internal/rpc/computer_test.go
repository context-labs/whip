package rpc_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestComputerRPCPassiveAvailabilityCASAndNoImplicitReconnect(t *testing.T) {
	_, c := fixture(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "unused-helper")
	marker := filepath.Join(dir, "unexpected-launch")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	first := call[protocol.ComputerStatus](t, c, "computer.status", protocol.EmptyParams{})
	if first.State != "disabled" || first.Configuration.Allow == nil || first.NativeConfigured {
		t.Fatal(first)
	}
	request := protocol.ConfigureComputerParams{Revision: first.Revision, Configuration: protocol.ComputerConfiguration{Enabled: true, HelperExecutable: binary, Allow: []string{" Test App "}, Deny: []string{}, DefaultDeny: true}}
	configured := call[protocol.ComputerStatus](t, c, "computer.configure", request)
	if configured.State != "available" || !configured.NativeConfigured || configured.Configuration.Allow[0] != "test app" {
		t.Fatal(configured)
	}
	var response json.RawMessage
	var wire *client.Error
	if err := c.Call(t.Context(), "computer.configure", request, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal("stale config accepted", err)
	}
	if err := c.Call(t.Context(), "computer.use_bundled", protocol.UseBundledComputerParams{Revision: configured.Revision}, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal("bundled setup replaced explicit helper", err)
	}
	retired := call[protocol.ComputerStatus](t, c, "computer.disconnect", protocol.ComputerConnectionParams{Generation: configured.Generation})
	if retired.State != "retired" {
		t.Fatal(retired)
	}
	again := call[protocol.ComputerStatus](t, c, "computer.status", protocol.EmptyParams{})
	if again.Generation != retired.Generation || again.State != "retired" {
		t.Fatal("status reconnected", again)
	}
	next := call[protocol.ComputerStatus](t, c, "computer.reconnect", protocol.ComputerConnectionParams{Generation: retired.Generation})
	if next.Generation == retired.Generation || next.State != "available" {
		t.Fatal(next)
	}
	if err := c.Call(t.Context(), "computer.reconnect", protocol.ComputerConnectionParams{Generation: retired.Generation}, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal("stale reconnect accepted", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("availability control launched helper", err)
	}
}
