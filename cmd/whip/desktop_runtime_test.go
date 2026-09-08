package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestDesktopRuntimeInfoDoesNotInitializeHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent-home")
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", filepath.Join(home, "whip"))
	t.Setenv("WHIP_DESKTOP_ASKPASS", "1")
	output := invokeMain(t, "_desktop-runtime-info")
	var metadata struct {
		BuildID       string `json:"buildId"`
		ProtocolMajor int    `json:"protocolMajor"`
		ProtocolMinor int    `json:"protocolMinor"`
		SchemaVersion int    `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(output), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.BuildID != version || metadata.ProtocolMajor != protocol.Major || metadata.ProtocolMinor != protocol.Minor {
		t.Fatal("runtime metadata did not match the compiled executable")
	}
	if metadata.SchemaVersion != session.SchemaVersion() || metadata.SchemaVersion < 1 {
		t.Fatal("runtime metadata did not report the supported schema version")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime metadata touched the home directory: %v", err)
	}
	if err := desktopRuntimeInfo([]string{"unexpected"}, io.Discard); err == nil {
		t.Fatal("runtime metadata accepted unexpected arguments")
	}
}
