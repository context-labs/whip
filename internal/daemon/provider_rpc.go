package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

type (
	ProviderLoginParams        = protocol.ProviderLoginParams
	ProviderLoginTeamParams    = protocol.ProviderLoginTeamParams
	ProviderLoginProjectParams = protocol.ProviderLoginProjectParams
	ProviderLoginCreateParams  = protocol.ProviderLoginCreateParams
)

func decodeProviderParams(data json.RawMessage, target any) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid provider operation parameters")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid provider operation parameters")
	}
	return nil
}

func (s *Server) handleProvider(connection *serverConn, request rpcMessage) (any, *RPCError, bool) {
	var result any
	var err error
	switch request.Method {
	case "provider.list":
		var p struct{}
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.ListProviders()
		}
	case "provider.disconnect":
		var p protocol.ProviderDisconnectParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.DisconnectProvider(connection.ctx, p)
		}
	case "config.get":
		var p struct{}
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.ReadConfiguration()
		}
	case "config.update":
		var p ConfigurationUpdate
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.UpdateConfiguration(p)
		}
	case "provider.key.set":
		var p ProviderKeySetup
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.SetProviderKey(connection.ctx, p)
		}
	case "provider.login.list":
		var p struct{}
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result = s.providers.ListLogins()
		}
	case "provider.status", "provider.logout", "provider.key.rotate":
		var p ProviderNameParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			switch request.Method {
			case "provider.status":
				result, err = s.providers.ProviderStatus(p.Provider)
			case "provider.logout":
				result, err = s.providers.LogoutProvider(connection.ctx, p.Provider)
			case "provider.key.rotate":
				result, err = s.providers.RotateProviderKey(connection.ctx, p.Provider)
			}
		}
	case "provider.login.begin":
		var p protocol.ProviderLoginBeginParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.BeginProviderLogin(p.Provider)
		}
	case "provider.login.status", "provider.login.cancel":
		var p ProviderLoginParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			if request.Method == "provider.login.status" {
				result, err = s.providers.LoginStatus(p.FlowID)
			} else {
				result, err = s.providers.CancelLogin(p.FlowID)
			}
		}
	case "provider.login.team.select":
		var p ProviderLoginTeamParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.SelectLoginTeam(p.FlowID, p.TeamID)
		}
	case "provider.login.project.select":
		var p ProviderLoginProjectParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.SelectLoginProject(p.FlowID, p.ProjectID)
		}
	case "provider.login.project.create":
		var p ProviderLoginCreateParams
		if err = decodeProviderParams(request.Params, &p); err == nil {
			result, err = s.providers.CreateLoginProject(p.FlowID, p.Name)
		}
	default:
		return nil, nil, false
	}
	if errors.Is(err, config.ErrRevisionConflict) {
		return nil, rpcFailure(-32009, err.Error()), true
	}
	return result, rpcFromError(err), true
}
