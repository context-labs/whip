package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

// Only public flow state is retained in the UI; device tokens and machine keys
// never cross the daemon boundary.
type inferenceNetPending struct {
	flowID          string
	state           string
	verificationURL string
}

type inferenceNetLoginMsg struct {
	status daemon.ProviderLoginStatus
	err    error
}

type inferenceNetKeyMsg struct{ err error }

func (m *model) authInferenceNetCommand(args []string) {
	if len(args) > 1 {
		m.authInferenceNetKey(config.TrimKey(strings.Join(args[1:], "")), false)
		return
	}
	m.authInferenceNetLogin()
}

func (m *model) authInferenceNetLogin() {
	if m.infAuth != nil {
		m.append(dimStyle.Render("Inference.net sign-in is already in progress"))
		return
	}
	m.append(dimStyle.Render("starting Inference.net sign-in… (approve in your browser)"))
	if m.prog == nil {
		return
	}
	m.infAuth = &inferenceNetPending{}
	client, program := m.client, m.prog
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+10*time.Second)
		defer cancel()
		status, err := client.BeginLogin(ctx)
		if err != nil {
			program.Send(inferenceNetLoginMsg{err: err})
			return
		}
		id := status.FlowID
		for {
			program.Send(inferenceNetLoginMsg{status: status})
			switch status.State {
			case "succeeded", "failed", "cancelled", "interrupted", "expired":
				return
			}
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			status, err = client.LoginStatus(ctx, id)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				// A request can race a disconnect. The next read waits for reconnection;
				// mutations and terminal input are never repeated here.
				status = daemon.ProviderLoginStatus{FlowID: id, State: "reconnecting"}
			}
		}
	}()
}

func (m *model) applyInferenceNetLogin(msg inferenceNetLoginMsg) bool {
	if msg.err != nil {
		m.append(errStyle.Render("Inference.net sign-in failed: " + msg.err.Error()))
		m.infAuth = nil
		return false
	}
	status := msg.status
	if m.infAuth == nil {
		return false
	}
	if m.infAuth.flowID != "" && m.infAuth.flowID != status.FlowID {
		return false
	}
	m.infAuth.flowID = status.FlowID
	if status.VerificationURL != "" && status.VerificationURL != m.infAuth.verificationURL {
		m.infAuth.verificationURL = status.VerificationURL
		m.append(dimStyle.Render("approve in your browser:\n  " + status.VerificationURL + "\n  code: " + status.UserCode))
		openBrowserURL(status.VerificationURL)
	}
	if status.State == "reconnecting" || status.State == m.infAuth.state {
		return false
	}
	m.infAuth.state = status.State
	switch status.State {
	case "choose_team":
		m.append(dimStyle.Render("✓ signed in as " + status.Email))
		if len(status.Teams) == 1 {
			m.selectInferenceNetTeam(status.FlowID, status.Teams[0].ID)
			return false
		}
		labels := providerChoiceLabels(status.Teams)
		m.openChoicePrompt("workspace:", labels, func(choice string) {
			for i, label := range labels {
				if choice == label {
					m.selectInferenceNetTeam(status.FlowID, status.Teams[i].ID)
					return
				}
			}
		})
	case "choose_project":
		labels := providerChoiceLabels(status.Projects)
		options := append(append([]string{}, labels...), "+ Create new project")
		m.openChoicePrompt("project:", options, func(choice string) {
			if choice == "+ Create new project" {
				m.createInferenceNetProject(status.FlowID)
				return
			}
			for i, label := range labels {
				if choice == label {
					m.inferenceNetMutation(func(ctx context.Context) (daemon.ProviderLoginStatus, error) {
						return m.client.SelectLoginProject(ctx, status.FlowID, status.Projects[i].ID)
					})
					return
				}
			}
		})
	case "loading_projects":
		m.append(dimStyle.Render("loading projects…"))
	case "provisioning":
		m.append(dimStyle.Render("provisioning a key on the execution host…"))
	case "succeeded":
		m.infAuth = nil
		m.append(dimStyle.Render("✓ signed in as " + status.Email + "; inference-net configured on the execution host"))
		return true
	case "failed", "expired", "cancelled", "interrupted":
		m.infAuth = nil
		m.append(errStyle.Render("Inference.net sign-in " + status.State + ". " + status.Error))
	}
	return false
}

// Include an ordinal because providers can return duplicate display names.
func providerChoiceLabels(choices []daemon.ProviderChoice) []string {
	labels := make([]string, len(choices))
	for i, choice := range choices {
		labels[i] = fmt.Sprintf("%d. %s", i+1, choice.Name)
	}
	return labels
}

func (m *model) selectInferenceNetTeam(id, teamID string) {
	client := m.client
	m.inferenceNetMutation(func(ctx context.Context) (daemon.ProviderLoginStatus, error) {
		return client.SelectLoginTeam(ctx, id, teamID)
	})
}

func (m *model) createInferenceNetProject(id string) {
	client := m.client
	m.openNamePrompt("new project name:", "", func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			m.inferenceNetMutation(func(ctx context.Context) (daemon.ProviderLoginStatus, error) { return client.CancelLogin(ctx, id) })
			return
		}
		m.inferenceNetMutation(func(ctx context.Context) (daemon.ProviderLoginStatus, error) {
			return client.CreateLoginProject(ctx, id, name)
		})
	})
}

func (m *model) inferenceNetMutation(operation func(context.Context) (daemon.ProviderLoginStatus, error)) {
	program := m.prog
	if program == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := operation(ctx)
		if err != nil {
			program.Send(noticeMsg("Inference.net selection could not be confirmed: " + err.Error() + ". Checking host state…"))
		}
		// The existing status reader is authoritative, avoiding out-of-order RPC
		// responses reopening an older picker after the flow has advanced.
	}()
}

func (m *model) authInferenceNetKey(key string, environment bool) {
	if key == "" && !environment {
		m.append(errStyle.Render("/auth inference-net <key> needs a key (get one at https://inference.net)"))
		return
	}
	m.append(dimStyle.Render("validating and saving the key on the execution host…"))
	if m.prog == nil {
		return
	}
	client, program := m.client, m.prog
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := setupProviderKey(ctx, client, config.InferenceNetProvider, key, environment)
		program.Send(inferenceNetKeyMsg{err: err})
	}()
}

func (m *model) applyInferenceNetKey(msg inferenceNetKeyMsg) bool {
	if msg.err != nil {
		m.append(errStyle.Render("Inference.net setup failed: " + msg.err.Error()))
		return false
	}
	m.append(dimStyle.Render("✓ inference-net configured on the execution host"))
	return true
}
