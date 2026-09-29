package rpc

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

func dispatchSkillRoots(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "host.skills.roots":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			value, err := r.HostConfiguration().Snapshot(ctx)
			return skillRoots(value), err
		})
	case "host.skills.publish":
		return decode(raw, func(p protocol.PublishSkillRootParams) (any, error) {
			value, err := r.HostConfiguration().PublishSkillRoot(ctx, p.ExpectedRevision, string(p.ID), p.Path)
			return skillRoots(value), err
		})
	case "host.skills.set_defaults":
		return decode(raw, func(p protocol.SetDefaultSkillRootsParams) (any, error) {
			roots := make([]string, len(p.Roots))
			for i, id := range p.Roots {
				roots[i] = string(id)
			}
			value, err := r.HostConfiguration().SetDefaultSkillRoots(ctx, p.ExpectedRevision, roots)
			return skillRoots(value), err
		})
	default:
		return nil, ErrMethod
	}
}

func skillRoots(value config.Snapshot) protocol.HostSkillRoots {
	result := protocol.HostSkillRoots{Revision: value.Revision, Roots: []protocol.HostSkillRoot{}, Defaults: []protocol.ID{}}
	for _, id := range slices.Sorted(maps.Keys(value.Host.SkillRoots)) {
		result.Roots = append(result.Roots, protocol.HostSkillRoot{ID: protocol.ID(id), Path: value.Host.SkillRoots[id]})
	}
	for _, id := range value.Host.Defaults.Instructions.SkillRoots {
		result.Defaults = append(result.Defaults, protocol.ID(id))
	}
	return result
}
