package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/computerconfig"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

func dispatchComputer(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "computer.status":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			value, err := r.ComputerStatus(ctx)
			return computerStatus(value), err
		})
	case "computer.configure":
		return decode(raw, func(p protocol.ConfigureComputerParams) (any, error) {
			c := p.Configuration
			value, err := r.ConfigureComputer(ctx, p.Revision, computerconfig.Config{Enabled: c.Enabled, HelperExecutable: c.HelperExecutable, Allow: c.Allow, Deny: c.Deny, DefaultDeny: c.DefaultDeny})
			return computerStatus(value), err
		})
	case "computer.reconnect", "computer.disconnect":
		return decode(raw, func(p protocol.ComputerConnectionParams) (any, error) {
			value, err := r.ChangeComputerConnection(ctx, string(p.Generation), method == "computer.reconnect")
			return computerStatus(value), err
		})
	}
	return nil, ErrMethod
}

func computerStatus(value runtime.ComputerStatus) protocol.ComputerStatus {
	c := value.Config
	return protocol.ComputerStatus{Revision: value.Revision, Configuration: protocol.ComputerConfiguration{Enabled: c.Enabled, HelperExecutable: c.HelperExecutable, Allow: append([]string{}, c.Allow...), Deny: append([]string{}, c.Deny...), DefaultDeny: c.DefaultDeny}, Generation: protocol.ID(value.Control.Generation), State: value.Control.State, NativeConfigured: value.Control.NativeConfigured, PlatformSupported: value.Control.PlatformSupported}
}
