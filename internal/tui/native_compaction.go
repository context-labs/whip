package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) compactionCommand(args string) tea.Cmd {
	owner := m.owner
	fields := strings.Fields(args)
	if len(fields) > 0 && fields[0] == "log" && len(fields) <= 2 {
		params := protocol.CompactionsParams{SessionID: owner.ID, Limit: 100}
		if len(fields) == 2 {
			params.After = new(protocol.ID(fields[1]))
		}
		return m.control("Retained compactions", false, func(ctx context.Context) nativeControlResult {
			var head protocol.ContextHead
			if err := m.connection.Call(ctx, "context.head", protocol.SessionParams{SessionID: owner.ID}, &head); err != nil {
				return nativeControlResult{err: err}
			}
			var page protocol.CompactionsResult
			if err := m.connection.Call(ctx, "context.compactions", params, &page); err != nil {
				return nativeControlResult{err: err}
			}
			if head.SessionID != owner.ID || len(page.Items) > params.Limit {
				return nativeControlResult{err: errors.New("compaction page ownership or bound mismatch")}
			}
			lines := []string{fmt.Sprintf("Context revision %d · /compact retry undoes the selected fold; summaries remain readable.", head.Revision)}
			for _, item := range page.Items {
				if item.SessionID != owner.ID {
					return nativeControlResult{err: errors.New("compaction ownership mismatch")}
				}
				selected := ""
				if head.CompactionID != nil && *head.CompactionID == item.ID {
					selected = " · selected"
				}
				lines = append(lines, fmt.Sprintf("%s · through %d · %d bytes%s", item.ID, item.ThroughSequence, item.TextBytes, selected))
			}
			if len(page.Items) == params.Limit {
				lines = append(lines, "Read another bounded page: /compact log "+string(page.Items[len(page.Items)-1].ID))
			}
			return nativeControlResult{notice: strings.Join(lines, "\n")}
		})
	}
	if args == "status" {
		return m.control("Compaction settings", false, func(ctx context.Context) nativeControlResult {
			var inventory protocol.ProviderInventory
			if err := m.connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
				return nativeControlResult{err: err}
			}
			handle, err := m.connection.Session(owner.ID)
			if err != nil {
				return nativeControlResult{err: err}
			}
			current, err := handle.Get(ctx)
			return nativeControlResult{owner: &current, notice: fmt.Sprintf("Host helper: %s\nSession helper: %s\nAutomatic compaction threshold: %d%%\n/compact <model> [provider] saves the host helper and this session; /compact off clears helper selection, retaining the threshold.", nativeCompactionModel(inventory.CompactionModel), nativeCompactionModel(current.Configuration.Compaction.Model), current.Configuration.Compaction.ThresholdPercent), err: err}
		})
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	if args == "" {
		command, err := m.connection.PrepareInput("sessions.compact", protocol.CompactParams{SessionID: owner.ID, Identity: protocol.RequestIdentity{ClientID: "tui", RequestID: protocol.ID(uuid.NewString())}})
		if err != nil {
			m.status = err.Error()
			return nil
		}
		return m.submitPreparedInput(command)
	}
	if args == "retry" {
		var params *protocol.SelectCompactionParams
		return m.control("Undo selected compaction", true, func(ctx context.Context) nativeControlResult {
			if params == nil {
				var head protocol.ContextHead
				if err := m.connection.Call(ctx, "context.head", protocol.SessionParams{SessionID: owner.ID}, &head); err != nil {
					return nativeControlResult{err: err}
				}
				if head.SessionID != owner.ID {
					return nativeControlResult{err: errors.New("context ownership mismatch")}
				}
				if head.CompactionID == nil {
					return nativeControlResult{label: "No selected compaction to undo"}
				}
				var selected protocol.CompactionResult
				if err := m.connection.Call(ctx, "context.compaction", protocol.CompactionParams{SessionID: owner.ID, CompactionID: *head.CompactionID}, &selected); err != nil {
					return nativeControlResult{err: err}
				}
				if selected.Metadata.SessionID != owner.ID || selected.Metadata.ID != *head.CompactionID {
					return nativeControlResult{err: errors.New("selected compaction ownership mismatch")}
				}
				params = &protocol.SelectCompactionParams{SessionID: owner.ID, ExpectedRevision: head.Revision, CompactionID: selected.Metadata.BaseID}
			}
			var updated protocol.ContextHead
			err := m.connection.Call(ctx, "context.select", *params, &updated)
			if err == nil && (updated.SessionID != owner.ID || !reflect.DeepEqual(updated.CompactionID, params.CompactionID)) {
				err = errors.New("updated context ownership or selection mismatch")
			}
			return nativeControlResult{notice: "Selected fold undone. Raw history and saved summaries remain intact; no model call was repeated.", err: err}
		})
	}
	return m.configureCompaction(args)
}

func nativeCompactionModel(value *protocol.ModelSelection) string {
	if value == nil {
		return "conversation model (no helper override)"
	}
	return string(value.Provider) + "/" + value.Name
}

func (m *nativeModel) configureCompaction(args string) tea.Cmd {
	fields := strings.Fields(args)
	if len(fields) < 1 || len(fields) > 2 || fields[0] == "off" && len(fields) != 1 || fields[0] == "log" || fields[0] == "retry" || fields[0] == "status" {
		m.status = "usage: /compact | log [after-id] | retry | status | off | <model> [provider]"
		return nil
	}
	owner := m.owner
	policy := owner.Configuration.Compaction
	policy.Model = nil
	if fields[0] != "off" {
		selection := protocol.ModelSelection{Provider: owner.Configuration.Model.Provider, Name: fields[0], Effort: "off"}
		if owner.Configuration.Compaction.Model != nil {
			selection.Provider = owner.Configuration.Compaction.Model.Provider
		}
		if len(fields) == 2 {
			selection.Provider = protocol.ID(fields[1])
		}
		policy.Model = &selection
	}
	var hostParams *protocol.ProviderDefaultsParams
	hostSaved := false
	sessionParams := protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{Compaction: &policy}}
	return m.control("Configure compaction", true, func(ctx context.Context) nativeControlResult {
		if hostParams == nil {
			var inventory protocol.ProviderInventory
			if err := m.connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
				return nativeControlResult{err: err}
			}
			hostParams = &protocol.ProviderDefaultsParams{Revision: inventory.Revision, Defaults: protocol.ProviderDefaults{Selection: policy.Model}}
		}
		if !hostSaved {
			var inventory protocol.ProviderInventory
			if err := m.connection.Call(ctx, "providers.compaction", *hostParams, &inventory); err != nil {
				return nativeControlResult{err: fmt.Errorf("host helper update failed or may be unknown; /compact status inspects both scopes: %w", err)}
			}
			if !reflect.DeepEqual(inventory.CompactionModel, policy.Model) {
				return nativeControlResult{err: errors.New("host compaction selection acknowledgment mismatch")}
			}
			hostSaved = true
		}
		var updated protocol.Session
		if err := m.connection.Call(ctx, "sessions.configure", sessionParams, &updated); err != nil {
			return nativeControlResult{err: fmt.Errorf("host helper default was saved; current session update failed or may be unknown; inspect /compact status: %w", err)}
		}
		if updated.ID != owner.ID || updated.ConfigRevision <= owner.ConfigRevision || !reflect.DeepEqual(updated.Configuration.Compaction, policy) {
			return nativeControlResult{err: errors.New("session compaction acknowledgment mismatch after host default was saved")}
		}
		return nativeControlResult{owner: &updated, notice: fmt.Sprintf("Host and session helper: %s\nAutomatic threshold remains %d%%.", nativeCompactionModel(policy.Model), policy.ThresholdPercent)}
	})
}
