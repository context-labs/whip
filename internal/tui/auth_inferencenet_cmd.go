package tui

import (
	"context"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
)

// /auth inference-net [key] connects Inference.net. The bare form runs the
// browser device-authorization login (no key handling); a pasted key goes
// through the same masked validate-and-upsert path as /auth openrouter.
//
// The flow is async: a goroutine runs each network step and reports back via
// messages, so the UI goroutine owns all appends/config writes. After sign-in
// the user picks the workspace + project through the input box (choice picker
// / namePrompt), and can create a project on the spot.

// inferenceNetPending holds the in-flight device-login state across the
// team → project → create prompts.
type inferenceNetPending struct {
	token string
	email string
	teams []inferencenet.Team
	team  inferencenet.Team
}

// authInferenceNetCommand dispatches the inference-net branch of /auth.
func (m *model) authInferenceNetCommand(args []string) {
	if len(args) > 1 {
		m.authInferenceNetKey(config.TrimKey(strings.Join(args[1:], "")), false)
		return
	}
	// Bare: browser device login (the smooth path). The paste-a-key route is
	// still available via `/auth inference-net <key>`.
	m.authInferenceNetLogin()
}

// inferenceNetAuthMsg carries a finished device login back to the UI.
type inferenceNetAuthMsg struct {
	auth inferencenet.Auth
	err  error
}

// authInferenceNetLogin runs the device-authorization flow in the background,
// then provisions a machine key and registers the provider.
func (m *model) authInferenceNetLogin() {
	m.append(dimStyle.Render("starting Inference.net sign-in… (approve in your browser)"))
	if m.prog == nil {
		return // tests drive applyInferenceNetAuth directly
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		sess, err := inferencenet.Login(ctx, func(verificationURL, userCode string) {
			m.prog.Send(noticeMsg("approve this terminal in your browser:\n  " + verificationURL + "\n  code: " + userCode))
			if openBrowserURL(verificationURL) {
				m.prog.Send(noticeMsg("browser opened; waiting for approval…"))
			}
		})
		if err != nil {
			m.prog.Send(inferenceNetLoginMsg{err: err})
			return
		}
		m.prog.Send(inferenceNetLoginMsg{email: sess.Email, teams: sess.Teams, token: sess.Token})
	}()
}

// inferenceNetLoginMsg carries a finished browser login (identity + the teams
// to pick from) back to the UI goroutine, which then prompts for team/project.
type inferenceNetLoginMsg struct {
	email string
	token string
	teams []inferencenet.Team
	err   error
}

// applyInferenceNetLogin starts the interactive team → project selection after
// the browser sign-in completes. Single-team users skip the team prompt.
func (m *model) applyInferenceNetLogin(msg inferenceNetLoginMsg) {
	if msg.err != nil {
		m.append(errStyle.Render("Inference.net sign-in failed: " + msg.err.Error()))
		return
	}
	m.infAuth = &inferenceNetPending{token: msg.token, email: msg.email, teams: msg.teams}
	m.append(dimStyle.Render("✓ signed in as " + msg.email))
	if len(msg.teams) == 1 {
		m.infAuth.team = msg.teams[0]
		m.inferenceNetPickProject()
		return
	}
	labels := make([]string, len(msg.teams))
	for i, t := range msg.teams {
		labels[i] = t.Name
		if t.Slug != "" {
			labels[i] += " (" + t.Slug + ")"
		}
	}
	m.openChoicePrompt("workspace:", labels, func(choice string) {
		for _, t := range m.infAuth.teams {
			l := t.Name
			if t.Slug != "" {
				l += " (" + t.Slug + ")"
			}
			if l == choice {
				m.infAuth.team = t
				break
			}
		}
		m.inferenceNetPickProject()
	})
}

// inferenceNetPickProject loads the team's projects, then offers a picker with
// a "+ Create new project" option.
func (m *model) inferenceNetPickProject() {
	m.append(dimStyle.Render("loading projects for " + m.infAuth.team.Name + "…"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		projects, err := inferencenet.ListProjects(ctx, m.infAuth.token, m.infAuth.team)
		m.prog.Send(inferenceNetProjectsMsg{projects: projects, err: err})
	}()
}

type inferenceNetProjectsMsg struct {
	projects []inferencenet.Project
	err      error
}

func (m *model) applyInferenceNetProjects(msg inferenceNetProjectsMsg) {
	if msg.err != nil {
		m.append(errStyle.Render("could not load projects: " + msg.err.Error()))
		m.infAuth = nil
		return
	}
	options := make([]string, 0, len(msg.projects)+1)
	for _, p := range msg.projects {
		options = append(options, p.Name)
	}
	options = append(options, inferencenet.CreateProjectOption)
	m.openChoicePrompt("project:", options, func(choice string) {
		if choice == inferencenet.CreateProjectOption {
			m.inferenceNetCreateProject()
			return
		}
		for _, p := range msg.projects {
			if p.Name == choice {
				m.inferenceNetFinish(p)
				return
			}
		}
	})
}

// inferenceNetCreateProject prompts for a name and creates the project.
func (m *model) inferenceNetCreateProject() {
	m.openNamePrompt("new project name:", "", func(name string) {
		name = config.TrimKey(name)
		if name == "" {
			m.append(dimStyle.Render("project creation cancelled"))
			m.infAuth = nil
			return
		}
		m.append(dimStyle.Render("creating project " + name + "…"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			p, err := inferencenet.CreateProject(ctx, m.infAuth.token, m.infAuth.team, name)
			m.prog.Send(inferenceNetProjectCreatedMsg{project: p, err: err})
		}()
	})
}

type inferenceNetProjectCreatedMsg struct {
	project inferencenet.Project
	err     error
}

func (m *model) applyInferenceNetProjectCreated(msg inferenceNetProjectCreatedMsg) {
	if msg.err != nil {
		m.append(errStyle.Render("could not create the project: " + msg.err.Error()))
		m.infAuth = nil
		return
	}
	m.inferenceNetFinish(msg.project)
}

// inferenceNetFinish mints the machine key under the chosen project, saves the
// auth state, and registers the provider.
func (m *model) inferenceNetFinish(p inferencenet.Project) {
	auth := inferencenet.Auth{
		SessionToken: m.infAuth.token,
		UserEmail:    m.infAuth.email,
		TeamID:       m.infAuth.team.ID,
		ProjectID:    p.ID,
		ProjectName:  p.Name,
	}
	m.infAuth = nil
	m.append(dimStyle.Render("provisioning an API key for this machine…"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := auth.EnsureMachineKey(ctx)
		if err == nil {
			err = inferencenet.SaveAuth(auth)
		}
		m.prog.Send(inferenceNetAuthMsg{auth: auth, err: err})
	}()
}

// applyInferenceNetAuth commits a finished device login on the UI goroutine:
// register the provider (machine key resolves from the stored auth file) and
// hot-swap the live agent when the session already routes inference-net.
func (m *model) applyInferenceNetAuth(msg inferenceNetAuthMsg) bool {
	if msg.err != nil {
		m.append(errStyle.Render("Inference.net sign-in failed: " + msg.err.Error()))
		return false
	}
	m.cfg.UpsertInferenceNet("", false) // machine key resolves from disk
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return false
	}
	m.append(dimStyle.Render("✓ signed in as " + msg.auth.UserEmail + " — project " + msg.auth.ProjectName + "; inference-net provider configured"))
	return true
}

// authInferenceNetKey validates a pasted Inference.net key and upserts the
// provider (mirrors the openrouter BYOK path).
func (m *model) authInferenceNetKey(key string, envMode bool) {
	if key == "" {
		m.append(errStyle.Render("/auth inference-net <key> needs a key (get one at https://inference.net)"))
		return
	}
	m.append(dimStyle.Render("validating key against Inference.net…"))
	if m.prog == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := m.client.ValidateProvider(ctx, daemon.ProviderValidateParams{
			Name: config.InferenceNetProvider, BaseURL: config.InferenceNetBaseURL, Key: key,
		})
		m.prog.Send(inferenceNetKeyMsg{key: key, envMode: envMode, models: result.Models, err: err})
	}()
}

type inferenceNetKeyMsg struct {
	key     string
	envMode bool
	models  []llm.ModelInfo
	err     error
}

func (m *model) applyInferenceNetKey(msg inferenceNetKeyMsg) bool {
	if msg.err != nil {
		m.append(errStyle.Render("Inference.net rejected the key: " + msg.err.Error()))
		return false
	}
	m.cfg.UpsertInferenceNet(msg.key, msg.envMode)
	if err := m.cfg.Save(); err != nil {
		m.append(errStyle.Render("config save failed: " + err.Error()))
		return false
	}
	m.append(dimStyle.Render("✓ inference-net configured; /model lists its models"))
	return true
}
