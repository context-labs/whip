package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/google/uuid"
)

type acpSession struct {
	id              acp.SessionId
	handle          *client.Session
	tree            protocol.ID
	lifecycle       context.Context
	stop            context.CancelFunc
	workers         sync.WaitGroup
	turnCh          chan struct{}
	observer        *client.Observer
	presentation    presentation
	mu              sync.Mutex
	closed          bool
	policy          protocol.PermissionPolicy
	cursor          client.ObservationCursor
	failure         error
	current         *client.InputCommand
	cancelRequested bool
	preparing       bool
	pending         map[protocol.ID]*decisionWork
	title           string
}

func (s *acpSession) close() { s.mu.Lock(); s.closed = true; s.stop(); s.mu.Unlock(); s.workers.Wait() }

func (b *Bridge) consume(s *acpSession) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	nextControls := time.Time{}
	for {
		page, err := s.observer.Next(s.lifecycle)
		var presentationErr error
		if err == nil {
			presentationErr = s.presentation.observe(page, false)
			err = presentationErr
		}
		s.mu.Lock()
		s.failure = err
		if err == nil {
			s.cursor = page.Cursor
		}
		s.mu.Unlock()
		if s.lifecycle.Err() != nil || presentationErr != nil {
			// An update can already be partly displayed. Never retry or advance
			// past a failed projection as if the omitted output were delivered.
			return
		}
		if page.Reset {
			return
		} // ACP cannot retract already displayed history.
		if err == nil && time.Now().After(nextControls) {
			if controlErr := b.controls(s); controlErr != nil && s.lifecycle.Err() == nil {
				logf("session %s controls: %v", s.id, controlErr)
			}
			nextControls = time.Now().Add(500 * time.Millisecond)
		}
		if err == nil && page.Cursor.After < page.Snapshot.ThroughSequence {
			continue
		}
		select {
		case <-s.lifecycle.Done():
			return
		case <-ticker.C:
		}
	}
}

func (b *Bridge) Prompt(parent context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	// The ACP SDK cancels the Prompt context before delivering session/cancel.
	// Keep one bounded observer owned by this attachment until settlement or
	// detach; only Cancel below has authority to cancel the durable host input.
	if err := parent.Err(); err != nil {
		return acp.PromptResponse{}, err
	}
	ctx, done, err := b.begin(context.WithoutCancel(parent))
	if err != nil {
		return acp.PromptResponse{}, err
	}
	defer done()
	s := b.getSession(p.SessionId)
	if s == nil {
		return acp.PromptResponse{}, acp.NewInvalidParams("unknown session")
	}
	select {
	case s.turnCh <- struct{}{}:
	default:
		return acp.PromptResponse{}, acp.NewInvalidParams("session busy: a prompt is already being observed")
	}
	defer func() { <-s.turnCh }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifecycle, cancel)
	defer stop()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return acp.PromptResponse{}, acp.NewInvalidParams("session is closed")
	}
	s.workers.Add(1)
	prior := s.current
	s.preparing = true
	s.cancelRequested = false
	s.mu.Unlock()
	defer s.workers.Done()
	defer func() { s.mu.Lock(); s.preparing = false; s.mu.Unlock() }()
	if prior != nil {
		value, found, err := prior.Check(ctx)
		if err != nil || !found || !terminal(value) {
			return acp.PromptResponse{}, acp.NewInvalidParams("previous input remains active or uncertain; cancel or inspect it before another prompt")
		}
	}
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
	content, err := preparePrompt(p.Prompt, b.options.Vision)
	if err != nil {
		return acp.PromptResponse{}, acp.NewInvalidParams(err.Error())
	}
	parts := []protocol.Part{}
	if content.text != "" {
		parts = append(parts, protocol.Part{Type: "text", Text: content.text})
	}
	for _, image := range content.images {
		ref, err := s.handle.PutContent(ctx, protocol.ID(uuid.NewString()), image.media, image.data)
		if err != nil {
			return acp.PromptResponse{}, acp.NewInternalError(err.Error())
		}
		parts = append(parts, protocol.Part{Type: "content", ReferenceID: ref.ID})
	}
	command, err := s.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "acp", RequestID: protocol.ID(uuid.NewString())}, Source: "user", Parts: parts})
	if err != nil {
		return acp.PromptResponse{}, acp.NewInvalidParams(err.Error())
	}
	s.mu.Lock()
	if s.cancelRequested {
		s.mu.Unlock()
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	}
	s.current = command
	s.mu.Unlock()
	_, sendErr := command.Send(ctx)
	if sendErr != nil {
		var found bool
		_, found, err = command.Check(ctx)
		if err != nil || !found {
			return acp.PromptResponse{}, acp.NewInternalError(fmt.Sprintf("input acceptance is uncertain; this adapter will not resubmit: %v", errors.Join(sendErr, err)))
		}
	}
	s.mu.Lock()
	cancelRequested := s.cancelRequested
	s.mu.Unlock()
	if cancelRequested {
		if err := b.cancelInput(ctx, s, command); err != nil {
			return acp.PromptResponse{}, acp.NewInternalError(err.Error())
		}
	}
	var admitted protocol.Admission
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		admitted, _, err = command.Check(ctx)
		if err != nil {
			return acp.PromptResponse{}, acp.NewInternalError(err.Error())
		}
		if admitted.Input == nil {
			return acp.PromptResponse{}, acp.NewInternalError("input was deleted")
		}
		if terminal(admitted) {
			break
		}
		s.mu.Lock()
		observationErr := s.failure
		s.mu.Unlock()
		if observationErr != nil {
			return acp.PromptResponse{}, acp.NewInternalError(observationErr.Error())
		}
		select {
		case <-ctx.Done():
			return acp.PromptResponse{}, acp.NewInternalError("prompt observation ended; host input continues until explicitly cancelled")
		case <-ticker.C:
		}
	}
	// Settlement precedes the snapshot; drain committed output through that fixed
	// high water before replying. A later turn cannot hold this response open.
	var snapshot protocol.HistorySnapshot
	if err := b.client.Call(ctx, "context.snapshot", protocol.SessionParams{SessionID: s.handle.ID()}, &snapshot); err != nil {
		return acp.PromptResponse{}, acp.NewInternalError(err.Error())
	}
	for {
		s.mu.Lock()
		cursor, observationErr := s.cursor, s.failure
		s.mu.Unlock()
		if observationErr != nil {
			return acp.PromptResponse{}, acp.NewInternalError(observationErr.Error())
		}
		if cursor.Revision != nil && *cursor.Revision != snapshot.Revision {
			return acp.PromptResponse{}, acp.NewInternalError("history changed; reload this session")
		}
		if cursor.After >= snapshot.ThroughSequence {
			break
		}
		select {
		case <-ctx.Done():
			return acp.PromptResponse{}, acp.NewInternalError(ctx.Err().Error())
		case <-ticker.C:
		}
	}
	response := acp.PromptResponse{StopReason: acp.StopReasonEndTurn, UserMessageId: p.MessageId, Meta: map[string]any{"whip_input_id": string(admitted.Input.ID)}}
	if response.UserMessageId == nil {
		response.UserMessageId = new(string(admitted.Input.ID))
	}
	if admitted.Input.State == "cancelled" || admitted.Turn != nil && admitted.Turn.State == "cancelled" {
		response.StopReason = acp.StopReasonCancelled
		b.accounting(ctx, s, admitted, &response)
		return response, nil
	}
	if admitted.Turn == nil || admitted.Turn.State != "succeeded" {
		failure := "input did not succeed"
		if admitted.Turn != nil {
			failure = "turn " + admitted.Turn.State
			if admitted.Turn.Failure != nil {
				failure = *admitted.Turn.Failure
			}
		}
		return acp.PromptResponse{}, acp.NewInternalError(failure)
	}
	b.accounting(ctx, s, admitted, &response)
	return response, nil
}

func terminal(value protocol.Admission) bool {
	return value.Input == nil || value.Input.State == "cancelled" || value.Turn != nil && value.Turn.State != "running" && value.Turn.State != "cancelling"
}

func (b *Bridge) Cancel(parent context.Context, p acp.CancelNotification) error {
	ctx, done, err := b.begin(parent)
	if err != nil {
		return err
	}
	defer done()
	s := b.getSession(p.SessionId)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.cancelRequested = true
	command := s.current
	preparing := s.preparing
	s.mu.Unlock()
	if preparing && command == nil {
		return nil
	}
	if command != nil {
		return b.cancelInput(ctx, s, command)
	}
	// A loaded session has no local command record. Capture the exact currently
	// active input once; never turn a subsequent race into a root-wide cancel.
	activity, err := s.handle.Activity(ctx)
	if err != nil {
		return err
	}
	if activity.ActiveInputID == nil {
		return nil
	}
	_, err = s.handle.CancelInput(ctx, *activity.ActiveInputID)
	return err
}

func (b *Bridge) cancelInput(ctx context.Context, s *acpSession, command *client.InputCommand) error {
	admitted, found, err := command.Check(ctx)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("input delivery is uncertain; cancellation has not been confirmed")
	}
	if admitted.Input == nil {
		return nil
	}
	_, err = s.handle.CancelInput(ctx, admitted.Input.ID)
	return err
}

func (s *acpSession) mode() acp.SessionModeId {
	s.mu.Lock()
	defer s.mu.Unlock()
	return acp.SessionModeId(acpPermissionMode(s.policy.Mode))
}
