package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
)

type nativeMenuPoll struct {
	menu       *nativeMenu
	generation uint64
}

func (m *nativeMenu) readAccount() tea.Cmd {
	connection, account := m.connection, m.setup.account
	return m.call("account-read", false, func(ctx context.Context) nativeMenuReply {
		reply := nativeMenuReply{}
		if account == "openai" {
			var status protocol.OpenAIAccountStatus
			if err := connection.Call(ctx, "accounts.openai.status", protocol.EmptyParams{}, &status); err != nil {
				return nativeMenuReply{err: err}
			}
			var flows protocol.OpenAIFlowsResult
			reply.err = connection.Call(ctx, "accounts.openai.list", protocol.EmptyParams{}, &flows)
			reply.openAIStatus, reply.openAIFlows = &status, flows.Items
		} else {
			var status protocol.InferenceAccountStatus
			if err := connection.Call(ctx, "accounts.inference.status", protocol.EmptyParams{}, &status); err != nil {
				return nativeMenuReply{err: err}
			}
			var flows protocol.InferenceFlowsResult
			reply.err = connection.Call(ctx, "accounts.inference.list", protocol.EmptyParams{}, &flows)
			reply.inferenceStatus, reply.inferenceFlows = &status, flows.Items
		}
		return reply
	})
}

func (m *nativeMenu) accountReply(reply nativeMenuReply) tea.Cmd {
	if reply.openAIStatus != nil {
		m.setup.openAIStatus = reply.openAIStatus
	}
	if reply.inferenceStatus != nil {
		m.setup.inferenceStatus = reply.inferenceStatus
	}
	switch reply.kind {
	case "account-read":
		m.setup.openAIFlows, m.setup.inferenceFlows = reply.openAIFlows, reply.inferenceFlows
		m.showAccount()
	case "account-status":
		m.showAccount()
		m.message += " " + reply.message
	case "account-flow":
		if reply.openAI != nil {
			m.setup.openAI = reply.openAI
		}
		if reply.inference != nil {
			m.setup.inference = reply.inference
		}
		m.showAccountFlow()
		if m.accountPending() {
			return m.accountPoll()
		}
	case "account-cleanup":
		m.mode, m.title = "account-cleanup", "Inference account cleanup"
		m.resetInput()
		m.choices = []nativeMenuChoice{{id: "retry-cleanup", label: "Retry known cleanup"}, {id: "back", label: "Back to account"}}
		for _, item := range reply.cleanup.Items {
			m.message += item.ID + ": key " + item.KeyState + ", session " + item.SessionState + ". " + nativeAccountText("Failure", item.Failure) + "\n"
		}
		if reply.cleanup.Failure != nil {
			m.message += *reply.cleanup.Failure
		}
		if len(reply.cleanup.Items) == 0 {
			m.message = "No retained cleanup operations."
		}
	}
	return nil
}

func (m *nativeMenu) showAccount() {
	m.mode, m.title = "account", "Account · "+m.setup.account
	m.resetInput()
	m.choices = []nativeMenuChoice{{id: "begin", label: "Start a new sign-in…"}, {id: "setup", label: "Publish route using known credentials", detail: "Retries local setup; does not create or rotate a key."}}
	if m.setup.account == "openai" {
		if status := m.setup.openAIStatus; status != nil {
			m.message = "Authentication: " + status.AuthState + "; route: " + status.RouteState + ". " + nativeAccountText("Account", status.Email) + nativeAccountText("Failure", status.Failure)
		}
		for _, flow := range m.setup.openAIFlows {
			m.choices = append(m.choices, nativeMenuChoice{id: "flow:" + flow.ID, label: "Inspect " + flow.State + " · " + flow.ID})
		}
	} else {
		if status := m.setup.inferenceStatus; status != nil {
			m.message = "Management credential: " + status.ManagementState + "; inference credential: " + status.InferenceState + "; route: " + status.RouteState + ". " + nativeAccountText("Team", status.TeamName) + nativeAccountText("Project", status.ProjectName) + nativeAccountText("Failure", status.Failure)
		}
		for _, flow := range m.setup.inferenceFlows {
			m.choices = append(m.choices, nativeMenuChoice{id: "flow:" + flow.ID, label: "Inspect " + flow.State + " · " + flow.ID})
		}
		m.choices = append(m.choices, nativeMenuChoice{id: "cleanup", label: "Inspect retained cleanup"}, nativeMenuChoice{id: "rotate", label: "Rotate inference key…"})
	}
	m.choices = append(m.choices, nativeMenuChoice{id: "logout", label: "Sign out…"}, nativeMenuChoice{id: "back", label: "Back to providers"})
	m.message += " Inference remains untested. Existing flow inspection never starts another login."
}

func (m *nativeMenu) showAccountFlow() {
	m.mode, m.title = "account-flow", "Account flow · "+m.setup.account
	m.resetInput()
	m.choices = []nativeMenuChoice{{id: "refresh", label: "Refresh flow"}, {id: "back", label: "Back to account"}}
	state := ""
	if m.setup.account == "openai" && m.setup.openAI != nil {
		flow := m.setup.openAI
		state = flow.State
		m.message = flow.ID + " · " + flow.State + ". " + nativeAccountText("Failure", flow.Failure)
		if state == "authorizing" {
			m.message += "\n" + nativeAccountText("Open", flow.VerificationURL) + "\n" + nativeAccountText("Code", flow.UserCode)
		}
		if state == "setup_required" {
			m.choices = append(m.choices, nativeMenuChoice{id: "setup", label: "Publish route using known credentials"})
		}
	} else if flow := m.setup.inference; flow != nil {
		state = flow.State
		m.message = flow.ID + " · " + flow.State + ". " + nativeAccountText("Failure", flow.Failure)
		switch state {
		case "authorizing":
			m.message += "\n" + nativeAccountText("Open", flow.VerificationURL) + "\n" + nativeAccountText("Code", flow.UserCode)
		case "choose_team":
			for _, team := range flow.Teams {
				m.choices = append(m.choices, nativeMenuChoice{id: "team:" + team.ID, label: team.Name + " · " + team.ID})
			}
		case "choose_project":
			for _, project := range flow.Projects {
				m.choices = append(m.choices, nativeMenuChoice{id: "project:" + project.ID, label: project.Name + " · " + project.ID})
			}
			m.choices = append(m.choices, nativeMenuChoice{id: "create-project", label: "Create a project…"})
		case "persistence_required", "setup_required", "cleanup_required":
			m.choices = append(m.choices, nativeMenuChoice{id: "retry-flow", label: "Retry the recorded " + state + " step", detail: "Uses the retained flow; never starts a new key provisioning attempt."})
		case "uncertain":
			m.message += "\nRemote outcome is unknown. Inspect the account and retained cleanup; do not start replacement provisioning automatically."
		}
	}
	switch state {
	case "authorizing", "choose_team", "loading_projects", "choose_project", "creating_project", "provisioning", "persistence_required", "setup_required", "cleanup_required", "uncertain":
		m.choices = append(m.choices, nativeMenuChoice{id: "cancel", label: "Cancel this exact flow"})
	}
}

func (m *nativeMenu) accountPending() bool {
	if m.setup.account == "openai" {
		return m.setup.openAI != nil && m.setup.openAI.State == "authorizing"
	}
	if m.setup.inference == nil {
		return false
	}
	switch m.setup.inference.State {
	case "authorizing", "loading_projects", "creating_project", "provisioning", "persistence_required", "setup_required", "cleanup_required":
		return true
	}
	return false
}

func (m *nativeMenu) refreshAccount() tea.Cmd {
	if m.mode == "account-flow" {
		if m.setup.account == "openai" && m.setup.openAI != nil {
			return m.accountFlowCall("get", protocol.OpenAIFlowParams{FlowID: m.setup.openAI.ID}, false)
		}
		if m.setup.inference != nil {
			return m.accountFlowCall("get", protocol.InferenceFlowParams{FlowID: m.setup.inference.ID}, false)
		}
	}
	return m.readAccount()
}

func (m *nativeMenu) accountFlowCall(action string, params any, mutation bool) tea.Cmd {
	connection, account := m.connection, m.setup.account
	return m.call("account-flow", mutation, func(ctx context.Context) nativeMenuReply {
		if account == "openai" {
			var flow protocol.OpenAILoginFlow
			err := connection.Call(ctx, "accounts.openai."+action, params, &flow)
			return nativeMenuReply{openAI: &flow, err: err}
		}
		var flow protocol.InferenceFlow
		err := connection.Call(ctx, "accounts.inference."+action, params, &flow)
		return nativeMenuReply{inference: &flow, err: err}
	})
}

func (m *nativeMenu) accountStatusCall(action string) tea.Cmd {
	connection, account := m.connection, m.setup.account
	return m.call("account-status", true, func(ctx context.Context) nativeMenuReply {
		if account == "openai" {
			var status protocol.OpenAIAccountStatus
			err := connection.Call(ctx, "accounts.openai."+action, protocol.EmptyParams{}, &status)
			return nativeMenuReply{openAIStatus: &status, err: err}
		}
		if action == "logout" {
			var result protocol.InferenceLogoutResult
			err := connection.Call(ctx, "accounts.inference.logout", protocol.EmptyParams{}, &result)
			return nativeMenuReply{inferenceStatus: &result.Status, message: nativeAccountText("Local failure", result.LocalFailure) + nativeAccountText("Cleanup failure", result.CleanupFailure), err: err}
		}
		var status protocol.InferenceAccountStatus
		err := connection.Call(ctx, "accounts.inference."+action, protocol.EmptyParams{}, &status)
		return nativeMenuReply{inferenceStatus: &status, err: err}
	})
}

func (m *nativeMenu) accountCleanup(retry bool) tea.Cmd {
	connection := m.connection
	method := "accounts.inference.cleanup"
	if retry {
		method = "accounts.inference.retry_cleanup"
	}
	return m.call("account-cleanup", retry, func(ctx context.Context) nativeMenuReply {
		var result protocol.InferenceCleanupResult
		err := connection.Call(ctx, method, protocol.EmptyParams{}, &result)
		return nativeMenuReply{cleanup: &result, err: err}
	})
}

func (m *nativeMenu) chooseAccount(choice nativeMenuChoice) tea.Cmd {
	switch m.mode {
	case "account":
		switch {
		case choice.id == "begin" || choice.id == "rotate" || choice.id == "logout":
			m.mode, m.title = "account-confirm", "Confirm account action"
			m.resetInput()
			m.choices = []nativeMenuChoice{{id: choice.id, label: choice.label}, {id: "back", label: "Back without changes"}}
			m.message = "This is an explicit account action. If delivery is uncertain, inspect existing flows before trying anything else."
		case choice.id == "setup":
			return m.accountStatusCall("setup")
		case choice.id == "cleanup":
			return m.accountCleanup(false)
		case choice.id == "back":
			return m.readSetup()
		case strings.HasPrefix(choice.id, "flow:"):
			id := strings.TrimPrefix(choice.id, "flow:")
			if m.setup.account == "openai" {
				return m.accountFlowCall("get", protocol.OpenAIFlowParams{FlowID: id}, false)
			}
			return m.accountFlowCall("get", protocol.InferenceFlowParams{FlowID: id}, false)
		}
	case "account-confirm":
		switch choice.id {
		case "back":
			return m.readAccount()
		case "logout":
			return m.accountStatusCall("logout")
		default:
			return m.accountFlowCall(choice.id, protocol.EmptyParams{}, true)
		}
	case "account-flow":
		switch {
		case choice.id == "back":
			return m.readAccount()
		case choice.id == "refresh":
			return m.refreshAccount()
		case choice.id == "setup":
			return m.accountStatusCall("setup")
		case choice.id == "cancel":
			if m.setup.account == "openai" {
				return m.accountFlowCall("cancel", protocol.OpenAIFlowParams{FlowID: m.setup.openAI.ID}, true)
			}
			return m.accountFlowCall("cancel", protocol.InferenceFlowParams{FlowID: m.setup.inference.ID}, true)
		case choice.id == "retry-flow":
			return m.accountFlowCall("retry", protocol.InferenceFlowParams{FlowID: m.setup.inference.ID}, true)
		case strings.HasPrefix(choice.id, "team:"):
			return m.accountFlowCall("team", protocol.InferenceTeamParams{FlowID: m.setup.inference.ID, TeamID: strings.TrimPrefix(choice.id, "team:")}, true)
		case strings.HasPrefix(choice.id, "project:"):
			return m.accountFlowCall("project", protocol.InferenceProjectParams{FlowID: m.setup.inference.ID, ProjectID: strings.TrimPrefix(choice.id, "project:")}, true)
		case choice.id == "create-project":
			m.setupForm("account-project-name", "New project name", "", false)
		}
	case "account-cleanup":
		if choice.id == "retry-cleanup" {
			return m.accountCleanup(true)
		}
		return m.readAccount()
	}
	return nil
}

func (m *nativeMenu) submitAccount(value string) tea.Cmd {
	if m.mode == "account-project-name" {
		return m.accountFlowCall("create_project", protocol.InferenceCreateProjectParams{FlowID: m.setup.inference.ID, Name: value}, true)
	}
	return nil
}

// The visible flow read timer shares the terminal work owner and is cancelled
// with the menu. Closing the terminal joins it alongside native RPC reads.
func (m *nativeMenu) accountPoll() tea.Cmd {
	generation, lifecycle, work := m.generation, m.lifecycle, m.work
	return func() tea.Msg {
		ctx, done, err := work.beginFor(2 * time.Second)
		if err != nil {
			return nil
		}
		defer done()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-lifecycle.Done():
			return nil
		case <-timer.C:
			return nativeMenuPoll{menu: m, generation: generation}
		}
	}
}
