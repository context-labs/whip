package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

const (
	nativeDecisionLimit = 128
	nativeDecisionBytes = 4 << 20
)

type nativeDecision struct {
	id, owner            protocol.ID
	root                 bool
	capability, resource string
	arguments            string
	question             *protocol.Question
}

type nativeDecisionPage struct{ items []nativeDecision }

type nativeDecisionDialog struct {
	value  nativeDecision
	offset int
	form   *nativeQuestionForm
}

// This is a bounded current observation, not a second permission queue. IDs and
// captured scopes come only from canonical pending rows; settlement stays native.
// Only roots can create human permission/question rows. Child operations lacking
// delegated authority settle denied instead; no tree/session scan is necessary.
func readNativeDecisions(ctx context.Context, connection *client.Client, attached protocol.Session) (*nativeDecisionPage, error) {
	page := &nativeDecisionPage{}
	bytes := 0
	value := attached
	handle, err := connection.Session(value.ID)
	if err != nil {
		return nil, err
	}
	activity, err := handle.Activity(ctx)
	if err != nil {
		return nil, err
	}
	if activity.PendingPermissionCount > nativeDecisionLimit || activity.PendingQuestionCount > nativeDecisionLimit {
		return nil, errors.New("owner has more than 128 pending decisions")
	}
	if activity.PendingPermissionCount > 0 {
		var after *protocol.ID
		for {
			var pending protocol.PermissionsResult
			if err := connection.Call(ctx, "permissions.list", protocol.PermissionsParams{SessionID: value.ID, PendingOnly: true, After: after, Limit: 100}, &pending); err != nil {
				return nil, err
			}
			for _, permission := range pending.Items {
				var operation protocol.HostOperation
				if err := connection.Call(ctx, "operations.get", protocol.HostOperationParams{OperationID: permission.OperationID}, &operation); err != nil {
					return nil, err
				}
				if permission.State != "pending" || operation.ID != permission.OperationID || operation.SessionID != value.ID {
					return nil, errors.New("pending permission ownership mismatch")
				}
				if operation.State != "waiting" {
					continue // Another client may settle between these reads.
				}
				arguments := string(operation.Arguments)
				if len(arguments) > nativeNoticeLimit {
					arguments = nativeBoundedNotice(arguments)
				}
				bytes += len(arguments) + len(operation.Capability) + len(operation.Resource)
				page.items = append(page.items, nativeDecision{id: operation.ID, owner: value.ID, root: value.ParentID == nil, capability: operation.Capability, resource: operation.Resource, arguments: arguments})
				if len(page.items) > nativeDecisionLimit || bytes > nativeDecisionBytes {
					return nil, errors.New("terminal decision observation exceeds 128 items or 4 MiB")
				}
			}
			if len(pending.Items) < 100 {
				break
			}
			next := pending.Items[len(pending.Items)-1].OperationID
			if after != nil && next <= *after {
				return nil, errors.New("permission cursor did not advance")
			}
			after = &next
		}
	}
	if activity.PendingQuestionCount > 0 {
		var after *protocol.ID
		for {
			var pending protocol.QuestionsResult
			if err := connection.Call(ctx, "questions.list", protocol.QuestionsParams{SessionID: value.ID, PendingOnly: true, After: after, Limit: 100}, &pending); err != nil {
				return nil, err
			}
			for _, question := range pending.Items {
				if question.SessionID != value.ID || question.State != "pending" {
					return nil, errors.New("pending question ownership mismatch")
				}
				encoded, err := json.Marshal(question.Request)
				if err != nil {
					return nil, err
				}
				bytes += len(encoded)
				page.items = append(page.items, nativeDecision{id: question.OperationID, owner: value.ID, root: value.ParentID == nil, question: &question})
				if len(page.items) > nativeDecisionLimit || bytes > nativeDecisionBytes {
					return nil, errors.New("terminal decision observation exceeds 128 items or 4 MiB")
				}
			}
			if len(pending.Items) < 100 {
				break
			}
			next := pending.Items[len(pending.Items)-1].OperationID
			if after != nil && next <= *after {
				return nil, errors.New("question cursor did not advance")
			}
			after = &next
		}
	}
	return page, nil
}

func (m *nativeModel) applyDecisions(page *nativeDecisionPage) {
	m.decisions = page.items
	if m.hiddenDecision != nil && !slices.ContainsFunc(m.decisions, func(value nativeDecision) bool { return value.id == m.hiddenDecision.value.id }) {
		m.hiddenDecision = nil
	}
	if m.decision != nil && !slices.ContainsFunc(m.decisions, func(value nativeDecision) bool { return value.id == m.decision.value.id }) {
		m.decision = nil // Canonical settlement closes presentation, never answers it.
	}
	if m.picker == nil && m.decision == nil && !m.decisionsHidden && len(m.decisions) > 0 && strings.TrimSpace(m.input.Value()) == "" && !m.sending && !m.controlling {
		m.decision = newNativeDecision(m.decisions[0], m.width)
	}
}

func (m *nativeModel) decidePermission(value nativeDecision, choice string) tea.Cmd {
	if !value.root && choice != "deny" {
		m.status = "Child authority requires an existing delegated grant; this dialog cannot mint it."
		return nil
	}
	if m.retryControl != nil {
		m.status = "The original decision outcome is uncertain; R explicitly retries that same decision."
		return nil
	}
	if choice != "once" && choice != "always" && choice != "deny" {
		m.status = "Unknown permission decision."
		return nil
	}
	params := protocol.ResolvePermissionParams{OperationID: value.id, Approved: choice != "deny"}
	grant := protocol.CreateGrantParams{ID: protocol.ID(uuid.NewString()), SessionID: value.owner, Capability: value.capability, Resource: value.resource}
	text := m.input.Value()
	checked := false
	command := m.control("Permission decision", true, func(ctx context.Context) nativeControlResult {
		if choice == "always" {
			if !checked {
				var current protocol.HostOperation
				if err := m.connection.Call(ctx, "operations.get", protocol.HostOperationParams{OperationID: value.id}, &current); err != nil {
					return nativeControlResult{err: err}
				}
				if current.ID != value.id || current.SessionID != value.owner || current.Capability != value.capability || current.Resource != value.resource || current.State != "waiting" {
					return nativeControlResult{err: &client.Error{Kind: "CONFLICT", Message: "The captured permission is no longer pending."}}
				}
				checked = true
			}
			var created protocol.Grant
			if err := m.connection.Call(ctx, "grants.create", grant, &created); err != nil {
				return nativeControlResult{err: err}
			}
			if created.ID != grant.ID || created.SessionID != value.owner || created.Capability != value.capability || created.Resource != value.resource {
				return nativeControlResult{err: errors.New("standing grant ownership mismatch")}
			}
			if created.RevokedAt != nil {
				return nativeControlResult{err: &client.Error{Kind: "CONFLICT", Message: "The original standing grant was revoked."}}
			}
		}
		var result protocol.Permission
		err := m.connection.Call(ctx, "permissions.resolve", params, &result)
		state := "denied"
		if params.Approved {
			state = "approved"
		}
		if err == nil && (result.OperationID != value.id || result.State != state) {
			err = errors.New("permission settlement did not match original decision")
		}
		return nativeControlResult{label: "Permission " + state, decisionID: value.id, err: err}
	})
	m.input.SetValue(text)
	return command
}

func (m *nativeModel) decisionKey(key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "esc" && (m.decision.form == nil || !m.decision.form.editing) {
		m.hiddenDecision = m.decision
		m.decision, m.decisionsHidden = nil, true
		return nil // Hiding a dialog never denies or dismisses host work.
	}
	if m.controlling {
		return nil
	}
	if strings.ToLower(key.String()) == "r" && m.retryControl != nil {
		m.controlling = true
		return m.retryControl
	}
	if m.retryControl != nil {
		m.status = "Decision outcome is uncertain; R retries only the original response, Esc hides the dialog."
		return nil
	}
	if m.decision.form != nil && key.String() != "pgup" && key.String() != "pgdown" {
		return m.questionKey(key)
	}
	switch strings.ToLower(key.String()) {
	case "pgdown":
		m.decision.offset += max(m.height-8, 1)
	case "pgup":
		m.decision.offset = max(0, m.decision.offset-max(m.height-8, 1))
	case "a", "s", "d":
		if m.decision.value.question != nil {
			return nil
		}
		choice := map[string]string{"a": "once", "s": "always", "d": "deny"}[strings.ToLower(key.String())]
		return m.decidePermission(m.decision.value, choice)
	}
	return nil
}

func (dialog *nativeDecisionDialog) view(width, height int) string {
	value := dialog.value
	body := fmt.Sprintf("Owner %s\nOperation %s\n\n%s\nResource: %s\n\nArguments:\n%s", value.owner, value.id, value.capability, value.resource, value.arguments)
	help := "A allow once · S allow exact scope for this root · D deny · Esc hide · PgUp/PgDn inspect"
	if !value.root {
		help = "Child requires delegated authority · D deny · Esc hide · PgUp/PgDn inspect"
	}
	if value.question != nil {
		body = "Owner " + string(value.owner) + "\n" + dialog.form.view(value.question.Request)
		help = "↑/↓ choose · Space select · F other · Enter next/send · D dismiss · ← previous · Esc hide"
	}
	lines := strings.Split(ansi.Hardwrap(nativeDisplayText(body), max(width-2, 1), true), "\n")
	help = ansi.Hardwrap(help, max(width-2, 1), true)
	window := max(height-len(strings.Split(help, "\n"))-3, 1)
	start := min(dialog.offset, max(len(lines)-window, 0))
	visible := strings.Join(lines[start:min(start+window, len(lines))], "\n")
	return "Human decision\n" + visible + "\n\n" + help
}
