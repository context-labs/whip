package acp

import (
	"context"
	"errors"
	"fmt"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/google/uuid"
)

const (
	optAllowOnce   = "allow-once"
	optAllowAlways = "allow-always"
	optReject      = "reject"
)

type decisionWork struct {
	cancel   context.CancelFunc
	answered bool
}

func (b *Bridge) controls(s *acpSession) error {
	var policy protocol.PermissionPolicy
	if err := b.client.Call(s.lifecycle, "permissions.policy", protocol.SessionParams{SessionID: s.handle.ID()}, &policy); err != nil {
		return err
	}
	if err := b.applyPermissionMode(s, policy); err != nil {
		return err
	}
	var tree protocol.Tree
	if err := b.client.Call(s.lifecycle, "trees.get", protocol.TreeParams{TreeID: s.tree}, &tree); err != nil {
		return err
	}
	title := ""
	if tree.Metadata.Title != nil {
		title = *tree.Metadata.Title
	}
	if s.title != title {
		s.title = title
		if err := b.update(s.lifecycle, s.id, acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{SessionUpdate: "session_info_update", Title: new(title)}}); err != nil {
			return err
		}
	}
	var summary protocol.TreeSummariesResult
	if err := b.client.Call(s.lifecycle, "trees.summaries", protocol.TreeSummariesParams{RootIDs: []protocol.ID{s.handle.ID()}}, &summary); err != nil {
		return err
	}
	if len(summary.Items) != 1 {
		return errors.New("attached root no longer exists")
	}
	counts := summary.Items[0].Activity
	seen := map[protocol.ID]bool{}
	if counts.PendingPermissionCount != 0 || counts.PendingQuestionCount != 0 {
		if counts.PendingPermissionCount+counts.PendingQuestionCount > 128 {
			return errors.New("ACP tree has more than 128 pending decisions; resolve them through native controls")
		}
		var after *protocol.ID
		for pages := 0; ; pages++ {
			if pages >= 11 {
				return errors.New("ACP tree decision scan exceeds 1024 sessions")
			}
			var sessions protocol.ListSessionsResult
			if err := b.client.Call(s.lifecycle, "sessions.list", protocol.ListSessionsParams{TreeID: s.tree, After: after, Limit: 100}, &sessions); err != nil {
				return err
			}
			for _, owner := range sessions.Items {
				if owner.TreeID != s.tree {
					return errors.New("session list crossed tree ownership")
				}
				if err := b.ownerDecisions(s, owner, seen); err != nil {
					return err
				}
			}
			if len(sessions.Items) < 100 {
				break
			}
			next := sessions.Items[len(sessions.Items)-1].ID
			if after != nil && next <= *after {
				return errors.New("session cursor did not advance")
			}
			after = &next
		}
	}
	s.mu.Lock()
	for id, work := range s.pending {
		if !seen[id] {
			work.cancel()
			delete(s.pending, id)
		}
	}
	s.mu.Unlock()
	return nil
}

func (b *Bridge) ownerDecisions(s *acpSession, owner protocol.Session, seen map[protocol.ID]bool) error {
	handle, err := b.client.Session(owner.ID)
	if err != nil {
		return err
	}
	activity, err := handle.Activity(s.lifecycle)
	if err != nil {
		return err
	}
	if activity.PendingPermissionCount > 0 {
		var after *protocol.ID
		for {
			var page protocol.PermissionsResult
			if err := b.client.Call(s.lifecycle, "permissions.list", protocol.PermissionsParams{SessionID: owner.ID, PendingOnly: true, After: after, Limit: 100}, &page); err != nil {
				return err
			}
			for _, permission := range page.Items {
				if permission.State != "pending" {
					return errors.New("pending permission query returned a terminal decision")
				}
				if len(seen) >= 128 {
					return errors.New("ACP pending decision capacity exceeded")
				}
				seen[permission.OperationID] = true
				b.startDecision(s, permission.OperationID, func(ctx context.Context) { b.handlePermission(ctx, s, owner, permission.OperationID) })
			}
			if len(page.Items) < 100 {
				break
			}
			next := page.Items[len(page.Items)-1].OperationID
			if after != nil && next <= *after {
				return errors.New("permission cursor did not advance")
			}
			after = &next
		}
	}
	if activity.PendingQuestionCount > 0 {
		var after *protocol.ID
		for {
			var page protocol.QuestionsResult
			if err := b.client.Call(s.lifecycle, "questions.list", protocol.QuestionsParams{SessionID: owner.ID, PendingOnly: true, After: after, Limit: 100}, &page); err != nil {
				return err
			}
			for _, question := range page.Items {
				if question.SessionID != owner.ID || question.State != "pending" {
					return errors.New("question ownership or state mismatch")
				}
				if len(seen) >= 128 {
					return errors.New("ACP pending decision capacity exceeded")
				}
				seen[question.OperationID] = true
				b.startDecision(s, question.OperationID, func(ctx context.Context) { b.handleQuestion(ctx, s, question) })
			}
			if len(page.Items) < 100 {
				break
			}
			next := page.Items[len(page.Items)-1].OperationID
			if after != nil && next <= *after {
				return errors.New("question cursor did not advance")
			}
			after = &next
		}
	}
	return nil
}

func (b *Bridge) startDecision(s *acpSession, id protocol.ID, handle func(context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.lifecycle.Err() != nil || s.pending[id] != nil || len(s.pending) >= 128 {
		return
	}
	running := 0
	for _, work := range s.pending {
		if !work.answered {
			running++
		}
	}
	if running >= 8 {
		return
	}
	select {
	case b.decisions <- struct{}{}:
	default:
		return
	}
	ctx, cancel := context.WithCancel(s.lifecycle)
	work := &decisionWork{cancel: cancel}
	s.pending[id] = work
	s.workers.Go(func() {
		defer func() { cancel(); <-b.decisions; s.mu.Lock(); work.answered = true; s.mu.Unlock() }()
		handle(ctx)
	})
}

// Human one-use grants can be minted only for roots. A child requires the
// existing delegated chain; an editor choice cannot replace that authority.
func permissionOptions(owner protocol.Session) []acp.PermissionOption {
	options := []acp.PermissionOption{{OptionId: optReject, Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce}}
	if owner.ParentID == nil {
		options = append([]acp.PermissionOption{{OptionId: optAllowOnce, Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}}, options...)
		options = append(options, acp.PermissionOption{OptionId: optAllowAlways, Name: "Always allow this exact capability and resource for this root", Kind: acp.PermissionOptionKindAllowAlways})
	}
	return options
}

func (b *Bridge) handlePermission(ctx context.Context, s *acpSession, owner protocol.Session, id protocol.ID) {
	var operation protocol.HostOperation
	if b.client.Call(ctx, "operations.get", protocol.HostOperationParams{OperationID: id}, &operation) != nil || operation.SessionID != owner.ID || operation.State != "waiting" {
		return
	}
	options := permissionOptions(owner)
	title := operation.Capability + " · " + operation.Resource
	if owner.ID != s.handle.ID() {
		title = "Child " + string(owner.ID) + " (approval requires delegated authority): " + title
	}
	request := acp.RequestPermissionRequest{SessionId: s.id, ToolCall: acp.ToolCallUpdate{ToolCallId: acp.ToolCallId("perm-" + id), Title: new(title), Kind: new(toolKind(operation.Capability)), Content: []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(string(operation.Arguments)))}}, Options: options}
	response, err := b.awaitDecision(ctx, request, func(ctx context.Context) bool {
		var current protocol.HostOperation
		return b.client.Call(ctx, "operations.get", protocol.HostOperationParams{OperationID: id}, &current) == nil && current.SessionID == owner.ID && current.State == "waiting"
	})
	if ctx.Err() != nil || err != nil {
		return
	} // Closing an editor is not a deny action.
	if owner.ParentID != nil && response.Outcome.Selected != nil && response.Outcome.Selected.OptionId != optReject {
		b.decisionError(s, "child authority requires an existing delegated grant", errors.New("editor selected an unavailable approval"))
		return
	}
	approved := err == nil && response.Outcome.Selected != nil && response.Outcome.Selected.OptionId == optAllowOnce
	if err == nil && response.Outcome.Selected != nil && response.Outcome.Selected.OptionId == optAllowAlways && owner.ParentID == nil {
		var grant protocol.Grant
		err = b.client.Call(ctx, "grants.create", protocol.CreateGrantParams{ID: protocol.ID(uuid.NewString()), SessionID: owner.ID, Capability: operation.Capability, Resource: operation.Resource}, &grant)
		if err != nil {
			b.decisionError(s, "grant outcome is uncertain; inspect native grants before retrying", err)
			return
		}
		approved = true
	}
	// Full Access does not suppress explicitly required connection grants.
	var result protocol.Permission
	if err := b.client.Call(ctx, "permissions.resolve", protocol.ResolvePermissionParams{OperationID: id, Approved: approved}, &result); err != nil {
		b.decisionError(s, "permission outcome requires inspection", err)
	}
}

// awaitDecision owns and joins its one ACP request. Canonical settlement in a
// different client or cancellation closes the editor prompt, without replaying
// an answer. Read failures fail closed and never grant permission.
func (b *Bridge) awaitDecision(parent context.Context, request acp.RequestPermissionRequest, pending func(context.Context) bool) (acp.RequestPermissionResponse, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	type result struct {
		response acp.RequestPermissionResponse
		err      error
	}
	conn, err := b.connection(ctx)
	if err != nil {
		return acp.RequestPermissionResponse{}, err
	}
	done := make(chan result, 1)
	go func() { response, err := conn.RequestPermission(ctx, request); done <- result{response, err} }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case response := <-done:
			if !pending(ctx) {
				return acp.RequestPermissionResponse{}, context.Canceled
			}
			return response.response, response.err
		case <-ctx.Done():
			cancel()
			<-done
			return acp.RequestPermissionResponse{}, ctx.Err()
		case <-ticker.C:
			if !pending(ctx) {
				cancel()
				<-done
				return acp.RequestPermissionResponse{}, context.Canceled
			}
		}
	}
}

func (b *Bridge) decisionError(s *acpSession, action string, err error) {
	if s.lifecycle.Err() == nil {
		_ = b.update(s.lifecycle, s.id, updateThoughtText(fmt.Sprintf("%s: %v\n", action, err)))
	}
}
