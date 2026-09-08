package main

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// desktopRuntimeInfo reads constants compiled into this executable. Packaging
// may call it before a user has a Whip home; it must not open a database/config.
func desktopRuntimeInfo(args []string, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("runtime build metadata accepts no arguments")
	}
	return json.NewEncoder(output).Encode(struct {
		Distribution  string `json:"distribution"`
		BuildID       string `json:"buildId"`
		UpdateOwner   string `json:"updateOwner"`
		ProtocolMajor int    `json:"protocolMajor"`
		ProtocolMinor int    `json:"protocolMinor"`
		SchemaVersion int    `json:"schemaVersion"`
	}{
		Distribution: buildinfo.Name, BuildID: version, UpdateOwner: buildinfo.UpdateOwner,
		ProtocolMajor: protocol.Major, ProtocolMinor: protocol.Minor, SchemaVersion: session.SchemaVersion(),
	})
}
