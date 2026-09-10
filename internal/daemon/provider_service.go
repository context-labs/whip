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
	"github.com/context-labs/whip/internal/openaiauth"
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
	provisionMu   sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	generation    string
	mu            sync.Mutex
	flows         map[string]*providerLoginFlow
	wg            sync.WaitGroup
	login         func(context.Context, func(string, string)) (providerLoginIdentity, error)
	projects      func(context.Context, string, inferencenet.Team) ([]inferencenet.Project, error)
	create        func(context.Context, string, inferencenet.Team, string) (inferencenet.Project, error)
	finish        func(context.Context, inferencenet.Auth) error
	validate      func(context.Context, string, string) ([]llm.ModelInfo, error)
	lifetime      time.Duration
	openAI        *openaiauth.Manager
	openAIErr     error
	openAILogin   func(context.Context, func(string, string)) (openaiauth.Credentials, error)
	refreshModels func(context.Context, string, config.Provider) error
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
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := inferenceNetLoginRoute(cfg); err != nil {
				return err
			}
			if _, err := auth.EnsureMachineKey(ctx); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := inferencenet.SaveAuth(auth); err != nil {
				return err
			}
			_, _, err = config.UpdateVersioned("", func(c *config.Config) error {
				if err := inferenceNetLoginRoute(c); err != nil {
					return err
				}
				c.UpsertInferenceNet("", false)
				c.EnableProvider(config.InferenceNetProvider)
				return nil
			})
			if err != nil {
				return err
			}
			return config.DeleteCatalog(config.InferenceNetProvider)
		},
		validate: validateProviderModels,
	}
	directory, err := config.Dir()
	if err != nil {
		s.openAIErr = errors.New("could not locate OpenAI credential directory on the execution host")
	} else {
		s.openAI = openaiauth.New(ctx, directory)
		s.openAILogin = func(ctx context.Context, onCode func(string, string)) (openaiauth.Credentials, error) {
			code, err := s.openAI.StartDevice(ctx)
			if err != nil {
				return openaiauth.Credentials{}, err
			}
			onCode(code.VerificationURL, code.UserCode)
			return s.openAI.CompleteDevice(ctx, code)
		}
	}
	s.refreshModels = s.refreshCatalog
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
	if s.openAI != nil {
		s.openAI.Close()
	}
}

func runtimeConfiguration(c *config.Config, revision string) RuntimeConfiguration {
	hosts := append([]config.RemoteHost{}, c.RemoteHosts...)
	claude, codex := true, true
	if c.MCPImport != nil {
		if c.MCPImport.Claude != nil && c.MCPImport.Claude.Enabled != nil {
			claude = *c.MCPImport.Claude.Enabled
		}
		if c.MCPImport.Codex != nil && c.MCPImport.Codex.Enabled != nil {
			codex = *c.MCPImport.Codex.Enabled
		}
	}
	disabled := append([]string{}, c.DisabledProviders...)
	return RuntimeConfiguration{
		DisabledProviders: &disabled,
		RemoteHosts:       &hosts,
		ImportClaude:      claude, ImportCodex: codex, Revision: revision, DefaultModel: c.DefaultModel,
		DefaultProvider: c.DefaultProvider, DefaultEffort: c.DefaultEffort,
		CompactModel: c.CompactModel, CompactProvider: c.CompactProvider, CompactPercent: c.CompactPct,
		GoalMaxRounds: c.GoalMaxRounds, MaxRetries: c.MaxRetries,
	}
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
	if p.DisabledProviders != nil {
		s.provisionMu.Lock()
		defer s.provisionMu.Unlock()
	}
	c, revision, err := config.UpdateVersioned(p.Revision, func(c *config.Config) error {
		if p.DisabledProviders != nil {
			if len(*p.DisabledProviders) > 128 {
				return errors.New("too many disabled providers")
			}
			names := slices.Clone(*p.DisabledProviders)
			for _, name := range names {
				if name == "" || len(name) > 256 || strings.ContainsAny(name, "\x00\r\n") {
					return errors.New("invalid provider ID")
				}
			}
			slices.Sort(names)
			c.DisabledProviders = slices.Compact(names)
		}
		if p.RemoteHosts != nil {
			hosts, err := config.NormalizeRemoteHosts(*p.RemoteHosts)
			if err != nil {
				return err
			}
			c.RemoteHosts = hosts
		}
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
		if p.DefaultEffort != nil {
			if err := validateConfiguredEffort(c, c.DefaultModel, c.DefaultProvider, *p.DefaultEffort); err != nil {
				return err
			}
		}
		if p.DefaultModel != nil || p.DefaultProvider != nil {
			selection := s.providerSelection(c, s.CatalogsFor(c), "", "")
			if !selection.Ready {
				return errors.New("select a model available on a connected provider before saving defaults")
			}
			if p.DefaultEffort == nil && validateConfiguredEffort(c, c.DefaultModel, c.DefaultProvider, c.DefaultEffort) != nil {
				c.DefaultEffort = ""
			}
		}
		return nil
	})
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	if p.DisabledProviders != nil {
		for _, name := range c.DisabledProviders {
			s.interruptProviderLogins(name)
		}
	}
	return runtimeConfiguration(c, revision), nil
}

func (s *ProviderService) SetProviderKey(ctx context.Context, p ProviderKeySetup) (RuntimeConfiguration, error) {
	if p.Revision == "" {
		return RuntimeConfiguration{}, errors.New("configuration revision is required")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	c, revision, err := config.ReadVersioned()
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	if revision != p.Revision {
		return RuntimeConfiguration{}, config.ErrRevisionConflict
	}
	provider, ok := c.Providers[p.Provider]
	for _, preset := range config.ProviderPresets() {
		if preset.ID != p.Provider {
			continue
		}
		if !ok {
			provider, ok = preset.Provider, true
		}
		if p.Environment && strings.TrimRight(provider.BaseURL, "/") == preset.Provider.BaseURL && (provider.APIKeyEnv == "" || slices.Contains(preset.EnvironmentVariables, provider.APIKeyEnv)) {
			provider.APIKeyEnv = config.DiscoverCredentials(c).AvailableEnvironmentVariable(preset)
		}
	}
	if !ok || (provider.API != "" && provider.API != "openai-completions") {
		return RuntimeConfiguration{}, errors.New("provider does not support API key setup")
	}
	credential := protocol.ProviderCredential{Mode: "api_key", Key: p.Key}
	if p.Environment {
		credential = protocol.ProviderCredential{Mode: "environment", EnvironmentVariable: provider.APIKeyEnv}
	}
	provider, err = applyProviderCredential(provider, credential, false)
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	if provider.Name == "" {
		provider.Name = p.Provider
	}
	if err := validateProviderDefinition(provider); err != nil {
		return RuntimeConfiguration{}, err
	}
	key, err := provider.ResolveKey(c)
	if err != nil || key == "" {
		return RuntimeConfiguration{}, errors.New("provider key is unavailable on the execution host")
	}
	validationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, discovery, err := s.discoverProviderModels(validationCtx, p.Provider, provider, key)
	if err != nil {
		if ctx.Err() != nil {
			return RuntimeConfiguration{}, ctx.Err()
		}
		return RuntimeConfiguration{}, providerValidationError(err)
	}
	if err := ctx.Err(); err != nil {
		return RuntimeConfiguration{}, err
	}
	c, revision, err = config.UpdateVersioned(p.Revision, func(c *config.Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return patchProviderConfiguration(c, p.Provider, provider, nil, false, true)
	})
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	s.interruptProviderLogins(p.Provider)
	// Named files can rotate without changing the configuration revision.
	// A delayed validation result must not publish models for an obsolete key.
	current, loadErr := config.Load()
	var currentKey string
	var keyErr error
	if loadErr == nil {
		currentKey, keyErr = provider.ResolveKey(current)
	}
	if loadErr == nil && keyErr == nil && current.Providers[p.Provider] == provider && currentKey == key {
		cacheProviderModels(p.Provider, provider.BaseURL, models)
	} else {
		discovery.Message += " Configuration saved, but credentials changed; refresh the model list."
	}
	result := runtimeConfiguration(c, revision)
	result.Discovery = &discovery
	return result, nil
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
	return s.BeginProviderLogin(config.InferenceNetProvider)
}

func (s *ProviderService) BeginProviderLogin(provider string) (ProviderLoginStatus, error) {
	if provider == "" {
		provider = config.InferenceNetProvider
	}
	if provider != config.InferenceNetProvider && provider != openaiauth.Provider {
		return ProviderLoginStatus{}, errors.New("provider does not support account login")
	}
	if provider == config.InferenceNetProvider {
		cfg, err := config.Load()
		if err != nil {
			return ProviderLoginStatus{}, err
		}
		if err := inferenceNetLoginRoute(cfg); err != nil {
			return ProviderLoginStatus{}, err
		}
	}
	if provider == openaiauth.Provider {
		if s.openAIErr != nil {
			return ProviderLoginStatus{}, s.openAIErr
		}
		cfg, err := config.Load()
		if err != nil {
			return ProviderLoginStatus{}, err
		}
		if err := cfg.UpsertOpenAICodex(); err != nil {
			return ProviderLoginStatus{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return ProviderLoginStatus{}, err
	}
	active := 0
	for id, flow := range s.flows {
		if loginActive(flow.status.State) {
			if flow.status.Provider == provider {
				return loginSnapshot(flow), nil
			}
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
	lifetime := s.lifetime
	if provider == openaiauth.Provider {
		lifetime = openaiauth.DeviceLifetime
	}
	ctx, cancel := context.WithTimeout(s.ctx, lifetime)
	flow := &providerLoginFlow{ctx: ctx, cancel: cancel, status: ProviderLoginStatus{
		FlowID: id, Provider: provider, State: "authorizing", Teams: []ProviderChoice{}, Projects: []ProviderChoice{},
		ExpiresAt: time.Now().Add(lifetime),
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
		if provider == openaiauth.Provider {
			s.loginOpenAI(flow)
			return
		}
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
		if len(flow.teams) == 1 && flow.teams[0].ID != "" {
			s.selectLoginTeam(flow, flow.teams[0])
		}
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
	s.selectLoginTeam(flow, flow.teams[index])
	return loginSnapshot(flow), nil
}

// selectLoginTeam starts discovery while the caller holds s.mu.
func (s *ProviderService) selectLoginTeam(flow *providerLoginFlow, selected inferencenet.Team) {
	flow.team, flow.status.TeamID, flow.status.State = selected, selected.ID, "loading_projects"
	token, team := flow.token, flow.team
	s.wg.Go(func() {
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
		if len(projects) == 1 && projects[0].ID != "" {
			s.provisionLogin(flow, projects[0], "")
		}
	})
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
	s.provisionLogin(flow, project, name)
	return loginSnapshot(flow), nil
}

// provisionLogin owns the final account setup and is called with s.mu held.
func (s *ProviderService) provisionLogin(flow *providerLoginFlow, project inferencenet.Project, name string) {
	flow.status.State = "provisioning"
	token, team, email := flow.token, flow.team, flow.status.Email
	s.wg.Go(func() {
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
	})
}
