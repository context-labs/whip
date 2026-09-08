package inferencenet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
)

// ValidateKey reports whether an API key is accepted by the inference
// gateway. GET /models is authenticated and free, so it's the cheap probe.
func ValidateKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("the gateway rejected the key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not validate the key (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// apiKeyNameMaxLength matches the relay's apiKeyNameSchema cap; the hostname
// absorbs it so the timestamp always survives (mirrors fast-cli).
const apiKeyNameMaxLength = 64

// machineKeyName builds `whip-<host>-<timestamp>`; the hostname absorbs the
// 64-char cap so the timestamp always survives.
func machineKeyName() string {
	hostname, _ := os.Hostname()
	ts := time.Now().UTC().Format(time.RFC3339)
	budget := apiKeyNameMaxLength - len("whip--"+ts)
	if len(hostname) > budget {
		hostname = hostname[:budget]
	}
	return "whip-" + hostname + "-" + ts
}

// CompleteLogin runs the device flow, then drives an interactive team/project
// picker (choose — prompting lets the caller create a project on the spot).
// The returned Auth has session + identity + the chosen project but no machine
// key yet — see EnsureMachineKey.
func CompleteLogin(ctx context.Context, onCode func(verificationURL, userCode string), choose ChooseFunc) (Auth, error) {
	sess, err := Login(ctx, onCode)
	if err != nil {
		return Auth{}, err
	}
	a := Auth{SessionToken: sess.Token, UserEmail: sess.Email}
	if err := a.pickWorkspace(ctx, sess, choose); err != nil {
		return Auth{}, err
	}
	return a, nil
}

// ChooseFunc picks one option from options. The CLI/TUI supply the
// interactive prompt; returning "" selects the first option.
type ChooseFunc func(kind, title string, options []string) (string, error)

// CreateProjectOption is the sentinel project-picker choice that creates a
// new project instead of selecting an existing one.
const CreateProjectOption = "+ Create new project"

// pickWorkspace resolves which team + project this machine works under,
// prompting when there is a real choice (or no project yet).
func (a *Auth) pickWorkspace(ctx context.Context, sess session, choose ChooseFunc) error {
	team := sess.Teams[0]
	if len(sess.Teams) > 1 && choose != nil {
		labels := make([]string, len(sess.Teams))
		for i, t := range sess.Teams {
			labels[i] = t.Name
			if t.Slug != "" {
				labels[i] += " (" + t.Slug + ")"
			}
		}
		picked, err := choose("team", "Choose a workspace", labels)
		if err != nil {
			return err
		}
		for _, t := range sess.Teams {
			l := t.Name
			if t.Slug != "" {
				l += " (" + t.Slug + ")"
			}
			if l == picked {
				team = t
				break
			}
		}
	}
	a.TeamID = team.ID

	projects, err := ListProjects(ctx, a.SessionToken, team)
	if err != nil {
		return err
	}
	if choose == nil {
		// Non-interactive: take the first project, or create "default".
		if len(projects) == 0 {
			p, err := CreateProject(ctx, a.SessionToken, team, "default")
			if err != nil {
				return err
			}
			projects = []Project{p}
		}
		a.ProjectID, a.ProjectName = projects[0].ID, projects[0].Name
		return nil
	}

	options := make([]string, 0, len(projects)+1)
	for _, p := range projects {
		options = append(options, p.Name)
	}
	options = append(options, CreateProjectOption)
	picked, err := choose("project", "Choose a project", options)
	if err != nil {
		return err
	}
	if picked == CreateProjectOption || picked == "" && len(projects) == 0 {
		return a.createProjectInteractive(ctx, team, choose)
	}
	for _, p := range projects {
		if p.Name == picked {
			a.ProjectID, a.ProjectName = p.ID, p.Name
			return nil
		}
	}
	// "" picks the first.
	if len(projects) > 0 {
		a.ProjectID, a.ProjectName = projects[0].ID, projects[0].Name
		return nil
	}
	return a.createProjectInteractive(ctx, team, choose)
}

// createProjectInteractive asks for a name and creates the project.
func (a *Auth) createProjectInteractive(ctx context.Context, team Team, choose ChooseFunc) error {
	name, err := choose("project-name", "Name the new project", nil)
	if err != nil {
		return err
	}
	if name == "" {
		name = "default"
	}
	p, err := CreateProject(ctx, a.SessionToken, team, name)
	if err != nil {
		return err
	}
	a.ProjectID, a.ProjectName = p.ID, p.Name
	return nil
}

// EnsureMachineKey returns the stored machine key, minting one when absent.
// It does not re-validate a stored key (the inference call itself is the
// check); Rotate forces a fresh key.
func (a *Auth) EnsureMachineKey(ctx context.Context) (string, error) {
	if a.MachineKey != "" {
		return a.MachineKey, nil
	}
	return a.mintMachineKey(ctx)
}

// Rotate archives the stored key (if any) and mints a replacement.
func (a *Auth) Rotate(ctx context.Context) (string, error) {
	if a.MachineKeyID != "" {
		_ = archiveAPIKey(ctx, a.SessionToken, a.TeamID, a.MachineKeyID) // best-effort
		a.MachineKeyID, a.MachineKey, a.MachineKeyName = "", "", ""
	}
	return a.mintMachineKey(ctx)
}

// ArchiveMachineKey disables the stored machine key (logout).
func (a *Auth) ArchiveMachineKey(ctx context.Context) error {
	if a.MachineKeyID == "" || a.SessionToken == "" || a.TeamID == "" {
		return nil
	}
	return archiveAPIKey(ctx, a.SessionToken, a.TeamID, a.MachineKeyID)
}

func (a *Auth) mintMachineKey(ctx context.Context) (string, error) {
	if a.SessionToken == "" || a.TeamID == "" || a.ProjectID == "" {
		return "", errors.New(buildinfo.Text("run `whip auth inference-net login` first"))
	}
	name := machineKeyName()
	id, key, err := createAPIKey(ctx, a.SessionToken, a.TeamID, a.ProjectID, name)
	if err != nil {
		return "", err
	}
	a.MachineKeyID, a.MachineKey, a.MachineKeyName = id, key, name
	return key, nil
}
