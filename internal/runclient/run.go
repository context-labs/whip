// Package runclient implements the CLI's one-turn workflow using only native
// protocol values. The runtime remains the authority for execution and history.
package runclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/google/uuid"
)

type Options struct {
	Resume                  protocol.ID
	Agent                   string
	Engine                  string
	WorkingDirectory        string
	Model, Provider, Effort string
	PermissionMode          string
	Configuration           protocol.RunConfiguration
	Prompt                  string
	CostNanoUSD, Tokens     protocol.Counter
	// Record saves the exact admission before any send. A failure stops admission.
	// It must not contain host credentials and is owned by the invoking client.
	Record func(client.InputRecord) error
}

type Result struct {
	SessionID protocol.ID
	Admission protocol.Admission
	Record    *client.InputRecord
	Text      string
}

// Run configures one idle session, admits one prompt, and observes its exact
// receipt. It never resends an uncertain admission or retries a stale CAS. An
// observing context ending is not cancellation; callers use Cancel explicitly.
func Run(ctx context.Context, c *client.Client, options Options, output *Output) (result Result, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	if c == nil || output == nil {
		return result, errors.New("run client and output are required")
	}
	if err := options.validate(); err != nil {
		return result, err
	}
	owner, err := configure(ctx, c, options)
	result.SessionID = owner.ID
	if err != nil {
		return result, err
	}
	s, err := c.Session(owner.ID)
	if err != nil {
		return result, err
	}
	var baseline protocol.HistorySnapshot
	if err = c.Call(ctx, "context.snapshot", protocol.SessionParams{SessionID: owner.ID}, &baseline); err != nil {
		return result, err
	}
	command, err := s.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "cli_run", RequestID: protocol.ID(uuid.NewString())}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: options.Prompt}}})
	if err != nil {
		return result, err
	}
	record := command.Record()
	result.Record = &record
	if options.Record != nil {
		if err = options.Record(record); err != nil {
			return result, err
		}
	}
	admitted, sendErr := command.Send(ctx)
	if sendErr != nil {
		// Read-only recovery may establish acceptance, never absence of an effect.
		var found bool
		admitted, found, err = command.Check(ctx)
		if err != nil || !found {
			return result, errors.Join(sendErr, err)
		}
	}
	result.Admission = admitted
	record = command.Record()
	result.Record = &record
	if options.Record != nil {
		if err = options.Record(record); err != nil {
			return result, err
		}
	}
	return observe(ctx, c, s, result, client.ObservationCursor{After: baseline.ThroughSequence, Revision: new(baseline.Revision)}, output)
}

// Recover checks the saved exact payload before observation. Only an explicit
// retry=true can reissue a missing original admission. Existing turns are never
// retargeted, and configuration is never changed during recovery.
func Recover(ctx context.Context, c *client.Client, raw []byte, retry bool, output *Output) (result Result, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	command, err := c.RestoreInput(raw)
	if err != nil {
		return result, err
	}
	record := command.Record()
	if record.Method != "sessions.submit" {
		return result, errors.New("run recovery requires a prompt submission")
	}
	result.Record = &record
	var found bool
	admitted, found, err := command.Check(ctx)
	if err != nil {
		return result, err
	}
	if !found {
		if !retry {
			return result, errors.New("input is not accepted; explicit retry is required")
		}
		admitted, err = command.Retry(ctx)
		if err != nil {
			return result, err
		}
	}
	if admitted.Input == nil {
		return result, errors.New("run was deleted")
	}
	result.SessionID, result.Admission = admitted.Input.SessionID, admitted
	record = command.Record()
	result.Record = &record
	s, err := c.Session(result.SessionID)
	if err != nil {
		return result, err
	}
	return observe(ctx, c, s, result, client.ObservationCursor{}, output)
}

func observe(ctx context.Context, c *client.Client, s *client.Session, result Result, cursor client.ObservationCursor, output *Output) (Result, error) {
	if result.Admission.Input == nil {
		return result, errors.New("run was deleted")
	}
	observer, err := s.Observer(cursor)
	if err != nil {
		return result, err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var current protocol.Admission
		if err := c.Call(ctx, "receipts.get", result.Admission.Receipt.Identity, &current); err != nil {
			return result, err
		}
		if current.Receipt.Identity != result.Admission.Receipt.Identity || current.Receipt.Digest != result.Admission.Receipt.Digest ||
			current.Input != nil && (current.Input.SessionID != result.SessionID || current.Receipt.InputID == nil || *current.Receipt.InputID != current.Input.ID || current.Input.ID != result.Admission.Input.ID) {
			return result, errors.New("run receipt identity changed")
		}
		result.Admission = current
		if current.Input == nil {
			return result, errors.New("run was deleted")
		}
		if current.Input.TurnID != nil && (current.Turn == nil || current.Turn.ID != *current.Input.TurnID || current.Turn.SessionID != result.SessionID) {
			return result, errors.New("run turn ownership mismatch")
		}
		if current.Turn == nil && current.Input.State != "cancelled" {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		// Read settlement before the history page: terminal implies all committed
		// messages already exist. Drain the fixed page high water before finishing.
		page, err := observer.Next(ctx)
		if err != nil {
			return result, err
		}
		if page.Reset {
			return result, errors.New("run history changed; reopen the saved run explicitly")
		}
		if current.Turn != nil {
			if err := output.Observe(page, current.Turn.ID); err != nil {
				return result, err
			}
			result.Text = output.FinalText()
		}
		terminal := current.Input.State == "cancelled" || current.Turn != nil && current.Turn.State != "running" && current.Turn.State != "cancelling"
		if terminal && page.Cursor.After >= page.Snapshot.ThroughSequence {
			output.DiscardPreview()
			if current.Turn == nil || current.Turn.State == "cancelled" {
				return result, errors.New("run cancelled")
			}
			if current.Turn.State != "succeeded" {
				if current.Turn.Failure != nil {
					return result, errors.New(*current.Turn.Failure)
				}
				return result, fmt.Errorf("run %s", current.Turn.State)
			}
			return result, output.Err()
		}
		if page.Cursor.After < page.Snapshot.ThroughSequence {
			continue
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Cancel is an explicit action separate from observation. It checks the original
// receipt even after a lost acknowledgement and cancels that input only. A
// missing receipt leaves delivery uncertain; it never cancels a different turn.
func Cancel(ctx context.Context, c *client.Client, result Result) error {
	if result.Record == nil {
		return nil
	}
	raw, err := json.Marshal(*result.Record)
	if err != nil {
		return err
	}
	command, err := c.RestoreInput(raw)
	if err != nil {
		return err
	}
	admitted, found, err := command.Check(ctx)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("input acceptance is still unknown; cancellation is not confirmed")
	}
	if admitted.Input == nil {
		return nil
	}
	s, err := c.Session(admitted.Input.SessionID)
	if err != nil {
		return err
	}
	_, err = s.CancelInput(ctx, admitted.Input.ID)
	return err
}

func configure(ctx context.Context, c *client.Client, options Options) (protocol.Session, error) {
	var owner protocol.Session
	var err error
	if options.Resume == "" {
		ref, err := c.ResolveDefinition(ctx, options.Agent)
		if err != nil {
			return owner, err
		}
		var mode *string
		if options.PermissionMode != "" {
			mode = new(options.PermissionMode)
		}
		creationID := protocol.ID(uuid.NewString())
		var created protocol.CreateTreeResult
		if err := c.Call(ctx, "trees.create", protocol.CreateTreeParams{CreationID: creationID, PermissionMode: mode, Definition: ref, Engine: options.Engine, WorkingDirectory: options.WorkingDirectory, Overrides: protocol.ConfigPatch{}}, &created); err != nil {
			return owner, fmt.Errorf("create session %s (do not assume missing after a transport error): %w", creationID, err)
		}
		if created.Root == nil || created.Deleted {
			return owner, errors.New("created session was deleted")
		}
		owner = *created.Root
	} else {
		s, err := c.Session(options.Resume)
		if err != nil {
			return owner, err
		}
		owner, err = s.Get(ctx)
		if err != nil {
			return owner, err
		}
		if options.Agent != "" && options.Agent != string(owner.Definition.ID) && options.Agent != string(owner.Definition.ID)+"@"+owner.Definition.Revision {
			return owner, fmt.Errorf("session uses agent %s; cannot resume with %s", owner.Definition.ID, options.Agent)
		}
		if options.Engine != "" {
			var tree protocol.Tree
			if err := c.Call(ctx, "trees.get", protocol.TreeParams{TreeID: owner.TreeID}, &tree); err != nil {
				return owner, err
			}
			if tree.Engine != options.Engine {
				return owner, fmt.Errorf("session uses %s; cannot resume with %s", tree.Engine, options.Engine)
			}
		}
		if options.PermissionMode != "" {
			return owner, errors.New("permission mode selects a new session; cannot combine with resume")
		}
	}
	var edit protocol.ControlEdit
	options.Configuration.Headless = true
	if options.Configuration.CacheKey == "" {
		options.Configuration.CacheKey = string(owner.ID)
	}
	err = c.Call(ctx, "run.configure", protocol.RunConfigureParams{ID: protocol.ID(uuid.NewString()), SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Configuration: options.Configuration}, &edit)
	if err != nil {
		return owner, err
	}
	if edit.Deleted || edit.Session == nil || edit.Session.ID != owner.ID {
		return owner, errors.New("configured session disappeared")
	}
	owner = *edit.Session
	if options.Model != "" || options.Provider != "" || options.Effort != "" {
		err = c.Call(ctx, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: new(selectModel(owner.Configuration.Model, options))}}, &owner)
		if err != nil {
			return owner, err
		}
	}
	if owner.Configuration.Model.Provider == "" || owner.Configuration.Model.Name == "" {
		return owner, errors.New("no model configured; select a native host provider and default model")
	}
	for _, cap := range []protocol.BudgetLimit{{Kind: "model_cost_nano_usd", Limit: &options.CostNanoUSD}, {Kind: "model_tokens", Limit: &options.Tokens}} {
		if *cap.Limit == 0 {
			continue
		}
		var budgets protocol.BudgetsResult
		if err = c.Call(ctx, "budgets.list", protocol.SessionParams{SessionID: owner.ID}, &budgets); err != nil {
			return owner, err
		}
		var revision protocol.Counter
		found := false
		for _, budget := range budgets.Items {
			if budget.SessionID == owner.ID && budget.Kind == cap.Kind {
				revision = budget.Revision
				found = true
			}
		}
		if !found {
			return owner, fmt.Errorf("missing native budget %s", cap.Kind)
		}
		var updated protocol.Budget
		if err = c.Call(ctx, "budgets.set", protocol.SetBudgetParams{SessionID: owner.ID, ExpectedRevision: revision, Budget: cap}, &updated); err != nil {
			return owner, err
		}
	}
	return owner, nil
}

func selectModel(current protocol.ModelSelection, options Options) protocol.ModelSelection {
	if options.Model != "" {
		current.Name = options.Model
	}
	if options.Provider != "" {
		current.Provider = protocol.ID(options.Provider)
	}
	if options.Effort != "" {
		current.Effort = options.Effort
	}
	return current
}

func (o Options) validate() error {
	if o.CostNanoUSD < 0 || o.Tokens < 0 {
		return errors.New("run budgets must be nonnegative")
	}
	if o.Resume != "" && o.PermissionMode != "" {
		return errors.New("permission mode selects a new session; cannot combine with resume")
	}
	if o.Engine != "" && o.Engine != "starlark" && o.Engine != "quickjs" {
		return errors.New("unknown execution engine")
	}
	if o.PermissionMode != "" && o.PermissionMode != "prompt" && o.PermissionMode != "automatic" {
		return errors.New("unknown permission mode")
	}
	if !utf8.ValidString(o.Prompt) || strings.ContainsRune(o.Prompt, 0) {
		return errors.New("run prompt requires valid UTF-8 without NUL")
	}
	if o.Prompt == "" {
		return errors.New("run prompt is required")
	}
	raw, err := json.Marshal(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "cli_run", RequestID: "validation"}, SessionID: "validation", Source: "user", Parts: []protocol.Part{{Type: "text", Text: o.Prompt}}})
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("run prompt exceeds the 1 MiB input document limit")
	}
	if err := protocol.Validate("SubmitParams", raw); err != nil {
		return err
	}
	raw, err = json.Marshal(protocol.RunConfigureParams{ID: "validation", SessionID: "validation", ExpectedRevision: 1, Configuration: o.Configuration})
	if err != nil {
		return err
	}
	return protocol.Validate("RunConfigureParams", raw)
}
