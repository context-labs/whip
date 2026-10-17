package client

import (
	"github.com/context-labs/whip/internal/commandpresentation"
	"github.com/context-labs/whip/internal/protocol"
)

func fillCommandPresentation(result *protocol.CommandResult) {
	result.Output, result.Error = commandpresentation.Decode(result.Operation, result.Result, result.Status)
	if result.Failure != nil {
		result.Error = result.Failure.Message
	}
}
