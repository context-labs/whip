package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

// RuntimeConfiguration contains settings safe to send to clients. Credentials
// and integration definitions stay on the execution host.
type RuntimeConfiguration = protocol.RuntimeConfiguration

type ConfigurationUpdate = protocol.ConfigurationUpdate

// ProviderKeySetup is ephemeral: never write it to a command journal or log.
type ProviderKeySetup = protocol.ProviderKeySetup

type ProviderChoice = protocol.ProviderChoice

type ProviderLoginStatus = protocol.ProviderLoginStatus

type providerLoginFlow struct {
	status   ProviderLoginStatus
	ctx      context.Context
	cancel   context.CancelFunc
	token    string
	teams    []inferencenet.Team
	projects []inferencenet.Project
	team     inferencenet.Team
}

type providerLoginIdentity struct {
	token string
	email string
	teams []inferencenet.Team
}

// ProviderService owns onboarding across connection lifetimes. Call Close during
// daemon shutdown. Provider failures are intentionally not returned verbatim:
// upstream error bodies may contain credentials.
type ProviderService struct {
	provisionMu sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	generation  string
	mu          sync.Mutex
	flows       map[string]*providerLoginFlow
	wg          sync.WaitGroup
	login       func(context.Context, func(string, string)) (providerLoginIdentity, error)
	projects    func(context.Context, string, inferencenet.Team) ([]inferencenet.Project, error)
	create      func(context.Context, string, inferencenet.Team, string) (inferencenet.Project, error)
	finish      func(context.Context, inferencenet.Auth) error
	validate    func(context.Context, string, string) ([]llm.ModelInfo, error)
	lifetime    time.Duration
}

func NewProviderService(ctx context.Context, generation string) *ProviderService {
	ctx, cancel := context.WithCancel(ctx)
	s := &ProviderService{
		ctx: ctx, cancel: cancel, generation: generation, flows: make(map[string]*providerLoginFlow),
		lifetime: 10 * time.Minute, projects: inferencenet.ListProjects, create: inferencenet.CreateProject,
		login: func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
			identity, err := inferencenet.Login(ctx, code)
			return providerLoginIdentity{token: identity.Token, email: identity.Email, teams: identity.Teams}, err
		},
		finish: func(ctx context.Context, auth inferencenet.Auth) error {
			if _, err := auth.EnsureMachineKey(ctx); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := inferencenet.SaveAuth(auth); err != nil {
				return err
			}
			_, _, err := config.UpdateVersioned("", func(c *config.Config) error { c.UpsertInferenceNet("", false); return nil })
			return err
		},
		validate: func(ctx context.Context, baseURL, key string) ([]llm.ModelInfo, error) {
			return llm.New(baseURL, key).Models(ctx)
		},
	}
	return s
}

func (s *ProviderService) Close() {
	s.mu.Lock()
	s.cancel()
	for _, flow := range s.flows {
		if loginActive(flow.status.State) {
			flow.status.State = "interrupted"
		}
		flow.token = ""
		flow.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func runtimeConfiguration(c *config.Config, revision string) RuntimeConfiguration {
	claude, codex := true, true
	if c.MCPImport != nil {
		if c.MCPImport.Claude != nil && c.MCPImport.Claude.Enabled != nil {
			claude = *c.MCPImport.Claude.Enabled
		}
		if c.MCPImport.Codex != nil && c.MCPImport.Codex.Enabled != nil {
			codex = *c.MCPImport.Codex.Enabled
		}
	}
	return RuntimeConfiguration{ImportClaude: claude, ImportCodex: codex, Revision: revision, DefaultModel: c.DefaultModel,
		DefaultProvider: c.DefaultProvider, DefaultEffort: c.DefaultEffort,
		CompactModel: c.CompactModel, CompactProvider: c.CompactProvider, CompactPercent: c.CompactPct,
		GoalMaxRounds: c.GoalMaxRounds, MaxRetries: c.MaxRetries}
}

func (s *ProviderService) ReadConfiguration() (RuntimeConfiguration, error) {
	c, revision, err := config.ReadVersioned()
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	return runtimeConfiguration(c, revision), nil
}

func (s *ProviderService) UpdateConfiguration(p ConfigurationUpdate) (RuntimeConfiguration, error) {
	if p.Revision == "" {
		return RuntimeConfiguration{}, errors.New("configuration revision is required")
	}
	c, revision, err := config.UpdateVersioned(p.Revision, func(c *config.Config) error {
		if p.ImportClaude != nil || p.ImportCodex != nil {
			if c.MCPImport == nil {
				c.MCPImport = &config.MCPImport{}
			}
			if p.ImportClaude != nil {
				if c.MCPImport.Claude == nil {
					c.MCPImport.Claude = &config.MCPImportSource{}
				}
				enabled := *p.ImportClaude
				c.MCPImport.Claude.Enabled = &enabled
			}
			if p.ImportCodex != nil {
				if c.MCPImport.Codex == nil {
					c.MCPImport.Codex = &config.MCPImportSource{}
				}
				enabled := *p.ImportCodex
				c.MCPImport.Codex.Enabled = &enabled
			}
		}

		if p.DefaultEffort != nil && !slices.Contains([]string{"", "off", "low", "medium", "high", "xhigh", "max"}, *p.DefaultEffort) {
			return errors.New("invalid default effort")
		}
		if p.CompactPercent != nil && (*p.CompactPercent < 0 || *p.CompactPercent > 100) {
			return errors.New("invalid compaction percentage")
		}
		if p.GoalMaxRounds != nil && *p.GoalMaxRounds < 0 {
			return errors.New("invalid goal round limit")
		}
		if p.MaxRetries != nil && *p.MaxRetries < 0 {
			return errors.New("invalid retry limit")
		}
		if p.DefaultModel != nil {
			c.DefaultModel = *p.DefaultModel
		}
		if p.DefaultProvider != nil {
			c.DefaultProvider = *p.DefaultProvider
		}
		if p.DefaultEffort != nil {
			c.DefaultEffort = *p.DefaultEffort
		}
		if p.CompactModel != nil {
			c.CompactModel = *p.CompactModel
		}
		if p.CompactProvider != nil {
			c.CompactProvider = *p.CompactProvider
		}
		if p.CompactPercent != nil {
			c.CompactPct = *p.CompactPercent
		}
		if p.GoalMaxRounds != nil {
			c.GoalMaxRounds = *p.GoalMaxRounds
		}
		if p.MaxRetries != nil {
			c.MaxRetries = *p.MaxRetries
		}
		return nil
	})
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	return runtimeConfiguration(c, revision), nil
}

func (s *ProviderService) SetProviderKey(ctx context.Context, p ProviderKeySetup) (RuntimeConfiguration, error) {
	if p.Revision == "" {
		return RuntimeConfiguration{}, errors.New("configuration revision is required")
	}
	c := config.Default()
	switch p.Provider {
	case "openrouter":
		c.UpsertOpenRouter(config.TrimKey(p.Key), p.Environment)
	case config.InferenceNetProvider:
		c.UpsertInferenceNet(config.TrimKey(p.Key), p.Environment)
	default:
		return RuntimeConfiguration{}, errors.New("unsupported provider")
	}
	provider := c.Providers[p.Provider]
	key, err := provider.ResolveKey()
	if err != nil || key == "" {
		return RuntimeConfiguration{}, errors.New("provider key is unavailable on the execution host")
	}
	validationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := s.validate(validationCtx, provider.BaseURL, key)
	if err != nil {
		return RuntimeConfiguration{}, errors.New("provider key validation failed")
	}
	if err := ctx.Err(); err != nil {
		return RuntimeConfiguration{}, err
	}
	c, revision, err := config.UpdateVersioned(p.Revision, func(c *config.Config) error {
		if c.Providers == nil {
			c.Providers = make(map[string]config.Provider)
		}
		c.Providers[p.Provider] = provider
		return nil
	})
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	cacheProviderModels(p.Provider, provider.BaseURL, models)
	return runtimeConfiguration(c, revision), nil
}

func loginActive(state string) bool {
	return state == "authorizing" || state == "choose_team" || state == "loading_projects" || state == "choose_project" || state == "provisioning"
}

func loginSnapshot(flow *providerLoginFlow) ProviderLoginStatus {
	status := flow.status
	status.Teams = append([]ProviderChoice{}, status.Teams...)
	status.Projects = append([]ProviderChoice{}, status.Projects...)
	return status
}

func (s *ProviderService) BeginLogin() (ProviderLoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return ProviderLoginStatus{}, err
	}
	active := 0
	for id, flow := range s.flows {
		if loginActive(flow.status.State) {
			active++
		} else if time.Now().After(flow.status.ExpiresAt) {
			delete(s.flows, id)
		}
	}
	if active >= 16 || len(s.flows) >= 64 {
		return ProviderLoginStatus{}, errors.New("provider login limit reached")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ProviderLoginStatus{}, err
	}
	id := s.generation + ":" + hex.EncodeToString(random[:])
	ctx, cancel := context.WithTimeout(s.ctx, s.lifetime)
	flow := &providerLoginFlow{ctx: ctx, cancel: cancel, status: ProviderLoginStatus{
		FlowID: id, State: "authorizing", Teams: []ProviderChoice{}, Projects: []ProviderChoice{}, ExpiresAt: time.Now().Add(s.lifetime),
	}}
	s.flows[id] = flow
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if loginActive(flow.status.State) {
			uncertain := flow.status.State == "provisioning"
			flow.status.State = "expired"
			if s.ctx.Err() != nil || uncertain {
				flow.status.State = "interrupted"
			}
		}
		flow.token = ""
	}()
	go func() {
		defer s.wg.Done()
		identity, err := s.login(ctx, func(url, code string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if flow.status.State == "authorizing" {
				flow.status.VerificationURL, flow.status.UserCode = url, code
			}
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		if flow.status.State != "authorizing" || ctx.Err() != nil {
			return
		}
		if err != nil {
			s.failLogin(flow)
			return
		}
		flow.token, flow.teams, flow.status.Email = identity.token, identity.teams, identity.email
		for _, team := range identity.teams {
			flow.status.Teams = append(flow.status.Teams, ProviderChoice{ID: team.ID, Name: team.Name})
		}
		flow.status.State = "choose_team"
	}()
	return loginSnapshot(flow), nil
}

func (s *ProviderService) LoginStatus(id string) (ProviderLoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil {
		if id != "" && !strings.HasPrefix(id, s.generation+":") {
			return ProviderLoginStatus{FlowID: id, State: "interrupted", Teams: []ProviderChoice{}, Projects: []ProviderChoice{}}, nil
		}
		return ProviderLoginStatus{}, errors.New("provider login is unavailable; start a new login")
	}
	return loginSnapshot(flow), nil
}

func (s *ProviderService) CancelLogin(id string) (ProviderLoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil {
		return ProviderLoginStatus{}, errors.New("unknown provider login")
	}
	if loginActive(flow.status.State) {
		if flow.status.State == "provisioning" {
			flow.status.State = "interrupted"
		} else {
			flow.status.State = "cancelled"
		}
		flow.token = ""
		flow.cancel()
	}
	return loginSnapshot(flow), nil
}

func (s *ProviderService) failLogin(flow *providerLoginFlow) {
	flow.status.State, flow.status.Error, flow.token = "failed", "provider operation failed; start a new login", ""
	flow.cancel()
}

func (s *ProviderService) SelectLoginTeam(id, teamID string) (ProviderLoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil || flow.status.State != "choose_team" || flow.ctx.Err() != nil {
		return ProviderLoginStatus{}, errors.New("login is not waiting for a team")
	}
	index := slices.IndexFunc(flow.teams, func(team inferencenet.Team) bool { return team.ID == teamID })
	if index < 0 {
		return ProviderLoginStatus{}, errors.New("unknown login team")
	}
	flow.team, flow.status.TeamID, flow.status.State = flow.teams[index], teamID, "loading_projects"
	token, team := flow.token, flow.team
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		projects, err := s.projects(flow.ctx, token, team)
		s.mu.Lock()
		defer s.mu.Unlock()
		if flow.status.State != "loading_projects" || flow.ctx.Err() != nil {
			return
		}
		if err != nil {
			s.failLogin(flow)
			return
		}
		flow.projects = projects
		for _, project := range projects {
			flow.status.Projects = append(flow.status.Projects, ProviderChoice{ID: project.ID, Name: project.Name})
		}
		flow.status.State = "choose_project"
	}()
	return loginSnapshot(flow), nil
}

func (s *ProviderService) SelectLoginProject(id, projectID string) (ProviderLoginStatus, error) {
	return s.completeLogin(id, projectID, "")
}

func (s *ProviderService) CreateLoginProject(id, name string) (ProviderLoginStatus, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 256 {
		return ProviderLoginStatus{}, errors.New("project name must contain 1 to 256 bytes")
	}
	return s.completeLogin(id, "", name)
}

func (s *ProviderService) completeLogin(id, projectID, name string) (ProviderLoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil || flow.status.State != "choose_project" || flow.ctx.Err() != nil {
		return ProviderLoginStatus{}, errors.New("login is not waiting for a project")
	}
	var project inferencenet.Project
	if name == "" {
		index := slices.IndexFunc(flow.projects, func(project inferencenet.Project) bool { return project.ID == projectID })
		if index < 0 {
			return ProviderLoginStatus{}, errors.New("unknown login project")
		}
		project = flow.projects[index]
	}
	flow.status.State = "provisioning"
	token, team, email := flow.token, flow.team, flow.status.Email
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		var err error
		if name != "" {
			project, err = s.create(flow.ctx, token, team, name)
		}
		if err == nil {
			s.provisionMu.Lock()
			if flow.ctx.Err() != nil {
				err = flow.ctx.Err()
			} else {
				err = s.finish(flow.ctx, inferencenet.Auth{SessionToken: token, UserEmail: email, TeamID: team.ID, ProjectID: project.ID, ProjectName: project.Name})
			}
			s.provisionMu.Unlock()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if flow.status.State != "provisioning" || flow.ctx.Err() != nil {
			return
		}
		if err != nil {
			s.failLogin(flow)
			return
		}
		flow.status.ProjectID, flow.status.State, flow.token = project.ID, "succeeded", ""
		flow.cancel()
	}()
	return loginSnapshot(flow), nil
}
