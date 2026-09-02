package daemon

import (
	"context"

	"github.com/context-labs/whip/internal/capability"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) DecidePermission(ctx context.Context, permissionID string, decision capability.Decision) (ticket capability.Ticket, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		admission, err := s.store.Pending(actorCtx, permissionID)
		if err != nil {
			return err
		}
		if admission.Request.RootID != s.meta.ID {
			return capability.ErrDenied
		}
		ticket, err = s.store.Decide(actorCtx, admission, permissionID, decision)
		return err
	})
	return ticket, err
}

func (s *Session) InspectCapability(ctx context.Context, callerAgentID, capabilityID string) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.InspectCapability(actorCtx, s.meta.ID, callerAgentID, capabilityID)
		return err
	})
	return record, err
}

func (s *Session) DelegateCapability(ctx context.Context, callerAgentID string, delegation sessionstore.CapabilityDelegation) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.DelegateCapability(actorCtx, s.meta.ID, callerAgentID, delegation)
		return err
	})
	return record, err
}

func (s *Session) RevokeCapability(ctx context.Context, callerAgentID, capabilityID string) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.RevokeCapabilityFor(actorCtx, s.meta.ID, callerAgentID, capabilityID)
		return err
	})
	return record, err
}
