package inferenceaccount

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/context-labs/whip/internal/inferenceauth"
)

func (s *Service) authorize(f *flow) error {
	var code struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		ExpiresIn  int64  `json:"expires_in"`
		Interval   int64  `json:"interval"`
	}
	if err := s.call(f.ctx, http.MethodPost, "/api/auth/device/code", "", "", map[string]string{"client_id": "whip"}, &code, false); err != nil {
		return err
	}
	if !token(code.DeviceCode) || !text(code.UserCode, 64, false) || code.ExpiresIn < 1 || code.ExpiresIn > 86400 || code.Interval < 0 || code.Interval > 60 {
		return &remoteError{}
	}
	s.mu.Lock()
	if !terminal(f.view.State) && f.ctx.Err() == nil {
		f.view.VerificationURL, f.view.UserCode = dashboardURL+"/device/approve?user_code="+url.QueryEscape(code.UserCode), code.UserCode
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(f.ctx, time.Duration(min(code.ExpiresIn, 900))*time.Second)
	defer cancel()
	interval := time.Duration(code.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	var access string
	for range 900 {
		if err := s.wait(ctx, interval); err != nil {
			return err
		}
		if err := s.guard(f); err != nil {
			return err
		}
		var response struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		status, err := s.request(ctx, http.MethodPost, "/api/auth/device/token", "", "", map[string]string{"client_id": "whip", "device_code": code.DeviceCode, "grant_type": "urn:ietf:params:oauth:grant-type:device_code"}, &response, false)
		if err != nil {
			return err
		}
		if status == http.StatusOK && token(response.AccessToken) {
			access = response.AccessToken
			break
		}
		switch response.Error {
		case "authorization_pending":
			if status != http.StatusBadRequest {
				return &remoteError{}
			}
		case "slow_down":
			if status != http.StatusBadRequest {
				return &remoteError{}
			}
			interval = min(interval+5*time.Second, time.Minute)
		case "expired_token":
			return context.DeadlineExceeded
		default:
			return &remoteError{}
		}
	}
	if access == "" {
		return context.DeadlineExceeded
	}
	var identity struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		Session struct {
			ExpiresAt *time.Time `json:"expiresAt"`
		} `json:"session"`
	}
	if err := s.call(f.ctx, http.MethodGet, "/api/auth/get-session?disableCookieCache=true", access, "", nil, &identity, false); err != nil {
		return err
	}
	if !text(identity.User.ID, 256, false) || !text(identity.User.Email, 1024, true) {
		return &remoteError{}
	}
	// A newly approved account never inherits another account's machine key.
	// Keep the old authority only for cleanup after a replacement is durable.
	credentials := inferenceauth.Credentials{Management: inferenceauth.Management{Token: access, UserID: identity.User.ID, Email: identity.User.Email, ExpiresAt: identity.Session.ExpiresAt}}
	s.mu.Lock()
	f.view.VerificationURL, f.view.UserCode = "", ""
	s.mu.Unlock()
	saved, err := s.install(f, credentials, "discover")
	if err != nil || !saved {
		return err
	}
	return s.discover(f)
}

func (s *Service) discover(f *flow) error {
	if !managementAvailable(f.credentials) {
		return ErrManagement
	}
	if err := s.guard(f); err != nil {
		return err
	}
	var teams []Team
	if err := s.call(f.ctx, http.MethodGet, "/api/auth/organization/list", f.credentials.Management.Token, "", nil, &teams, false); err != nil {
		return err
	}
	if len(teams) == 0 || len(teams) > 256 {
		return &remoteError{}
	}
	seen := map[string]bool{}
	for _, team := range teams {
		if !text(team.ID, 256, false) || !text(team.Name, 512, true) || !text(team.Slug, 256, true) || seen[team.ID] {
			return &remoteError{}
		}
		seen[team.ID] = true
	}
	s.mu.Lock()
	if s.auth.Generation() != f.generation {
		s.mu.Unlock()
		return inferenceauth.ErrChanged
	}
	if terminal(f.view.State) || f.ctx.Err() != nil {
		s.mu.Unlock()
		return context.Canceled
	}
	f.view.Teams = teams
	f.view.State = ChooseTeam
	if len(teams) == 1 {
		f.view.TeamID = teams[0].ID
		f.view.State = LoadingProjects
	}
	s.mu.Unlock()
	if len(teams) == 1 {
		return s.projects(f)
	}
	return nil
}

func (s *Service) SelectTeam(ctx context.Context, id, teamID string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	f := s.flows[id]
	if f == nil {
		return Flow{}, ErrNotFound
	}
	if f.busy || f.view.State != ChooseTeam && f.view.State != ChooseProject {
		return Flow{}, ErrBusy
	}
	found := false
	for _, team := range f.view.Teams {
		found = found || team.ID == teamID
	}
	if !found {
		return Flow{}, ErrInvalid
	}
	f.view.TeamID, f.view.ProjectID, f.view.Projects = teamID, "", nil
	s.start(f, LoadingProjects, s.projects)
	return clone(f.view), nil
}

func (s *Service) projects(f *flow) error {
	if err := s.guard(f); err != nil {
		return err
	}
	if !managementAvailable(f.credentials) {
		return ErrManagement
	}
	token, team := f.credentials.Management.Token, f.view.TeamID
	if err := s.activate(f.ctx, token, team); err != nil {
		return err
	}
	var projects []Project
	if err := s.call(f.ctx, http.MethodGet, "/api/rest/projects", token, team, nil, &projects, false); err != nil {
		return err
	}
	if len(projects) > 256 {
		return &remoteError{}
	}
	seen := map[string]bool{}
	for _, project := range projects {
		if !text(project.ID, 256, false) || !text(project.Name, 512, true) || seen[project.ID] {
			return &remoteError{}
		}
		seen[project.ID] = true
	}
	s.mu.Lock()
	if s.auth.Generation() != f.generation {
		s.mu.Unlock()
		return inferenceauth.ErrChanged
	}
	if terminal(f.view.State) || f.ctx.Err() != nil {
		s.mu.Unlock()
		return context.Canceled
	}
	f.view.Projects, f.view.State = projects, ChooseProject
	if len(projects) == 1 {
		f.view.ProjectID = projects[0].ID
		f.view.State = Provisioning
	}
	s.mu.Unlock()
	if len(projects) == 1 {
		return s.provision(f)
	}
	return nil
}

func (s *Service) SelectProject(ctx context.Context, id, projectID string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	f := s.flows[id]
	if f == nil {
		return Flow{}, ErrNotFound
	}
	if f.busy || f.view.State != ChooseProject {
		return Flow{}, ErrBusy
	}
	found := false
	for _, project := range f.view.Projects {
		found = found || project.ID == projectID
	}
	if !found {
		return Flow{}, ErrInvalid
	}
	f.view.ProjectID = projectID
	s.start(f, Provisioning, s.provision)
	return clone(f.view), nil
}

// CreateProject accepts one explicit creation for this waiting flow. Once
// accepted, the state leaves ChooseProject; duplicate/replayed calls cannot mint
// another project, including after a lost or uncertain response.
func (s *Service) CreateProject(ctx context.Context, id, name string) (Flow, error) {
	if !text(name, 512, false) {
		return Flow{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	f := s.flows[id]
	if f == nil {
		return Flow{}, ErrNotFound
	}
	if f.busy || f.view.State != ChooseProject {
		return Flow{}, ErrBusy
	}
	s.start(f, CreatingProject, func(f *flow) error {
		if !managementAvailable(f.credentials) {
			return ErrManagement
		}
		if err := s.activate(f.ctx, f.credentials.Management.Token, f.view.TeamID); err != nil {
			return err
		}
		if err := s.guard(f); err != nil {
			return err
		}
		s.mu.Lock()
		f.creating = true
		s.mu.Unlock()
		var project Project
		if err := s.call(f.ctx, http.MethodPost, "/api/rest/projects/create", f.credentials.Management.Token, f.view.TeamID, map[string]string{"name": name}, &project, true); err != nil {
			return err
		}
		if !text(project.ID, 256, false) || !text(project.Name, 512, true) {
			return &remoteError{uncertain: true}
		}
		s.mu.Lock()
		if s.auth.Generation() != f.generation {
			s.mu.Unlock()
			return inferenceauth.ErrChanged
		}
		if terminal(f.view.State) || f.ctx.Err() != nil {
			s.mu.Unlock()
			return context.Canceled
		}
		f.view.Projects, f.view.ProjectID, f.view.State = []Project{project}, project.ID, Provisioning
		s.mu.Unlock()
		return s.provision(f)
	})
	return clone(f.view), nil
}

// Retry resumes only a known local publication/setup or idempotent old-key
// cleanup. It never starts a new device exchange, project creation or key mint.
func (s *Service) Retry(ctx context.Context, id string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	f := s.flows[id]
	if f == nil {
		return Flow{}, ErrNotFound
	}
	if f.busy {
		return Flow{}, ErrBusy
	}
	var work func(*flow) error
	switch f.view.State {
	case PersistenceRequired:
		work = s.persist
	case SetupRequired:
		work = s.complete
	case CleanupRequired:
		work = s.cleanupKey
	default:
		return Flow{}, ErrInvalid
	}
	s.start(f, f.view.State, work)
	return clone(f.view), nil
}

func (s *Service) install(f *flow, credentials inferenceauth.Credentials, resume string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(f.ctx); err != nil {
		return false, err
	}
	if terminal(f.view.State) || s.auth.Generation() != f.generation {
		return false, inferenceauth.ErrChanged
	}
	if resume == "discover" && !s.addCleanup(f.previous) {
		return false, ErrLimit
	}
	f.credentials, f.pending, f.resume = credentials, true, resume
	err := s.auth.Install(f.ctx, f.generation, credentials)
	if errors.Is(err, inferenceauth.ErrChanged) {
		return false, err
	}
	f.published = s.auth.Generation() == f.generation+1
	if f.published {
		f.generation++
	}
	if err != nil {
		if errors.Is(err, inferenceauth.ErrStorage) || errors.Is(err, inferenceauth.ErrStoragePending) {
			f.view.State, f.view.Failure = PersistenceRequired, "Known credentials need local persistence; retry this flow"
			return false, nil
		}
		return false, err
	}
	f.pending = false
	f.creating = false
	return true, nil
}

func (s *Service) persist(f *flow) error {
	if f.published {
		s.mu.Lock()
		if s.auth.Generation() != f.generation {
			s.mu.Unlock()
			return inferenceauth.ErrChanged
		}
		_, err := s.auth.Capture(f.ctx)
		if errors.Is(err, inferenceauth.ErrKeyRequired) {
			err = nil
		}
		if err == nil {
			_, err = s.auth.Snapshot()
		}
		if err != nil {
			f.view.State, f.view.Failure = PersistenceRequired, ErrCredentials.Error()
			s.mu.Unlock()
			return ErrCredentials
		}
		f.pending = false
		f.creating = false
		s.mu.Unlock()
	} else {
		saved, err := s.install(f, f.credentials, f.resume)
		if err != nil || !saved {
			return err
		}
	}
	if err := s.guard(f); err != nil {
		return err
	}
	if f.resume == "discover" {
		return s.discover(f)
	}
	return s.complete(f)
}
