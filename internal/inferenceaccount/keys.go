package inferenceaccount

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
)

type cleanupRecord struct {
	id                   string
	expires              time.Time
	credentials          inferenceauth.Credentials
	keyDone, sessionDone bool
	sessionRetained      bool
	failure              bool
}

// Cleanup is a bounded, explicitly unverified remote cleanup outcome. Records
// expire after the same retention interval as flows; expiration never means the
// remote key/session was removed. No new account supplies their authorization.
type Cleanup struct {
	ID           string    `json:"id"`
	ExpiresAt    time.Time `json:"expires_at"`
	TeamID       string    `json:"team_id"`
	KeyID        string    `json:"key_id"`
	KeyState     string    `json:"key_state"`
	SessionState string    `json:"session_state"`
	Failure      string    `json:"failure"`
}

func (r cleanupRecord) complete() bool {
	return (r.keyDone || r.credentials.MachineKey.ID == "") && (r.sessionDone || r.sessionRetained || r.credentials.Management.Token == "")
}

func (s *Service) provision(f *flow) error {
	if err := s.guard(f); err != nil {
		return err
	}
	if !managementAvailable(f.credentials) {
		return ErrManagement
	}
	managementToken, team, project := f.credentials.Management.Token, f.view.TeamID, f.view.ProjectID
	if err := s.activate(f.ctx, managementToken, team); err != nil {
		return err
	}
	if err := s.guard(f); err != nil {
		return err
	}
	name := "whip-" + time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	f.creating = true
	s.mu.Unlock()
	input := map[string]any{"defaultProjectId": project, "name": name, "teamId": team, "scopes": []map[string]any{{"permissions": []string{"read", "write"}, "projectId": project}}}
	var key struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := s.call(f.ctx, http.MethodPost, "/api/rest/api-keys", managementToken, team, input, &key, true); err != nil {
		return err
	}
	if !text(key.ID, 256, false) || !token(key.Key) {
		return &remoteError{uncertain: true}
	}
	credentials := f.credentials
	credentials.Scope = inferenceauth.Scope{TeamID: team, ProjectID: project}
	for _, value := range f.view.Teams {
		if value.ID == team {
			credentials.Scope.TeamName = value.Name
		}
	}
	for _, value := range f.view.Projects {
		if value.ID == project {
			credentials.Scope.ProjectName = value.Name
		}
	}
	credentials.MachineKey = inferenceauth.MachineKey{ID: key.ID, Value: key.Key, Name: name}
	saved, err := s.install(f, credentials, "complete")
	if err != nil || !saved {
		return err
	}
	return s.complete(f)
}

func (s *Service) complete(f *flow) error {
	if err := s.guard(f); err != nil {
		return err
	}
	if err := s.setup(f.ctx); err != nil {
		s.mu.Lock()
		if !terminal(f.view.State) {
			f.view.State, f.view.Failure = SetupRequired, ErrSetup.Error()
		}
		s.mu.Unlock()
		return nil
	}
	return s.cleanupKey(f)
}

func (s *Service) cleanupKey(f *flow) error {
	if err := s.guard(f); err != nil {
		return err
	}
	if f.view.Kind == "login" {
		// Replacement is already durable. Failures stay independently visible
		// without trapping a new account behind the prior account's authority.
		_ = s.remoteCleanup(f.ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !terminal(f.view.State) {
			s.finish(f, Succeeded, "")
		}
		return nil
	}
	previous := f.previous
	if previous.MachineKey.ID != "" && previous.MachineKey.ID != f.credentials.MachineKey.ID {
		if err := s.archive(f.ctx, previous.Management.Token, previous.Scope.TeamID, previous.MachineKey.ID); err != nil {
			s.mu.Lock()
			if !terminal(f.view.State) {
				f.view.State, f.view.Failure = CleanupRequired, "New key is saved; old key cleanup needs attention"
			}
			s.mu.Unlock()
			return nil
		}
		s.mu.Lock()
		for i := range s.cleanup {
			r := &s.cleanup[i]
			if r.credentials.Management.Token == previous.Management.Token && r.credentials.Scope.TeamID == previous.Scope.TeamID && r.credentials.MachineKey.ID == previous.MachineKey.ID {
				r.keyDone, r.failure = true, false
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !terminal(f.view.State) {
		s.finish(f, Succeeded, "")
	}
	return nil
}

// Logout revokes local authorization before joining network work. Remote
// cleanup is bounded and separately reported; retries reuse only the prior
// cleanup record, never reauthorize locally or repeat creation.
func (s *Service) Logout(ctx context.Context) (LogoutResult, error) {
	return s.logout(ctx, func(clear func() error) error { _ = clear(); return nil })
}

// LogoutProvider reuses logout while checking route revision and shared account
// references under the existing login lock. Remote cleanup remains outside it.
func (s *Service) LogoutProvider(ctx context.Context, authority *config.Authority, revision, id string) (LogoutResult, config.ProviderDisconnect, error) {
	var result config.ProviderDisconnect
	status, err := s.logout(ctx, func(clear func() error) error {
		var err error
		result, err = authority.DisconnectProvider(ctx, revision, id, "inference-net", clear)
		return err
	})
	return status, result, err
}

func (s *Service) logout(ctx context.Context, guard func(func() error) error) (LogoutResult, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return LogoutResult{}, err
	}
	done := make([]<-chan struct{}, 0, len(s.flows))
	retained := true
	var localErr error
	err := guard(func() error {
		s.prune()
		s.loggingOut = true
		for _, f := range s.flows {
			if !terminal(f.view.State) {
				retained = s.addCleanup(f.previous) && retained
				if f.pending {
					retained = s.addCleanup(f.credentials) && retained
				}
				s.finish(f, Interrupted, "")
				f.cancel()
			}
			done = append(done, f.done)
			if !f.busy {
				f.credentials, f.previous = inferenceauth.Credentials{}, inferenceauth.Credentials{}
			}
		}
		prior, err := s.auth.Logout()
		localErr = err
		s.logoutPending = localErr != nil
		retained = s.addCleanup(prior) && retained
		return localErr
	})
	if err != nil || !s.loggingOut {
		result := LogoutResult{Status: s.status(), Cleanup: s.cleanupViews()}
		s.mu.Unlock()
		return result, err
	}
	s.wg.Add(1)
	defer s.wg.Done()
	s.mu.Unlock()
	for _, finished := range done {
		<-finished
	}
	remoteCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	var cleanupErr error
	if len(s.cleanup) != 0 {
		s.control.Lock()
		cleanupErr = s.remoteCleanup(remoteCtx)
		s.control.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loggingOut = false
	result := LogoutResult{Status: s.status(), Cleanup: s.cleanupViews()}
	if localErr != nil {
		result.LocalFailure = "Local sign-out is not durable; retry logout"
	}
	if cleanupErr != nil {
		result.CleanupFailure = "Local authorization was revoked; remote cleanup needs attention"
	}
	if !retained {
		result.CleanupFailure = "Local authorization was revoked; some remote cleanup could not be retained"
	}
	return result, nil
}

// addCleanup requires mu. The active flow contributes at most an old and a
// known replacement key. Completed cleanup flags survive local logout retries.
func (s *Service) addCleanup(value inferenceauth.Credentials) bool {
	if value.Management.Token == "" && value.MachineKey.ID == "" {
		return true
	}
	for _, record := range s.cleanup {
		prior := record.credentials
		if prior.Management.Token == value.Management.Token && prior.Scope.TeamID == value.Scope.TeamID && prior.MachineKey.ID == value.MachineKey.ID {
			return true
		}
	}
	if len(s.cleanup) >= maxFlows {
		return false
	}
	value.MachineKey.Value = "" // Remote cleanup needs the identifier, never inference authorization.
	s.cleanup = append(s.cleanup, cleanupRecord{id: s.epoch + ":" + rand.Text(), expires: time.Now().Add(retention), credentials: value})
	return true
}

func (s *Service) remoteCleanup(ctx context.Context) error {
	s.mu.Lock()
	records := slices.Clone(s.cleanup)
	s.mu.Unlock()
	var result error
	for _, record := range records {
		if record.keyDone {
			continue
		}
		value := record.credentials
		s.mu.Lock()
		current, _, captureErr := s.capture()
		localRevoked := s.loggingOut
		s.mu.Unlock()
		if value.MachineKey.ID != "" && value.MachineKey.ID == current.MachineKey.ID && value.Scope.TeamID == current.Scope.TeamID {
			continue
		}
		if captureErr != nil && !localRevoked {
			result = errors.Join(result, ErrCredentials)
			continue
		}
		if err := s.archive(ctx, value.Management.Token, value.Scope.TeamID, value.MachineKey.ID); err != nil {
			s.updateCleanup(record.id, func(r *cleanupRecord) { r.failure = true })
			result = errors.Join(result, ErrCredentials)
			continue
		}
		s.updateCleanup(record.id, func(r *cleanupRecord) { r.keyDone, r.failure = true, false })
	}
	// Sign out only after every key has been archived, including a prior rotation
	// awaiting cleanup. Otherwise sign-out could destroy its cleanup authority.
	for _, record := range records {
		management := record.credentials.Management.Token
		if management == "" {
			continue
		}
		s.mu.Lock()
		current, _, captureErr := s.capture()
		localRevoked := s.loggingOut
		pendingKey := false
		alreadyDone := false
		for _, other := range s.cleanup {
			pendingKey = pendingKey || other.credentials.Management.Token == management && !other.keyDone && other.credentials.MachineKey.ID != ""
			alreadyDone = alreadyDone || other.credentials.Management.Token == management && other.sessionDone
		}
		if current.Management.Token == management {
			for i := range s.cleanup {
				if s.cleanup[i].credentials.Management.Token == management {
					s.cleanup[i].sessionRetained = true
				}
			}
		}
		s.mu.Unlock()
		if pendingKey || alreadyDone || current.Management.Token == management {
			continue
		}
		if captureErr != nil && !localRevoked {
			result = errors.Join(result, ErrCredentials)
			continue
		}
		status, err := s.request(ctx, http.MethodPost, "/api/auth/sign-out", management, "", map[string]string{}, nil, false)
		if err != nil {
			s.updateCleanup(record.id, func(r *cleanupRecord) { r.failure = true })
			result = errors.Join(result, ErrCredentials)
			continue
		}
		if status != http.StatusOK && status != http.StatusUnauthorized {
			s.updateCleanup(record.id, func(r *cleanupRecord) { r.failure = true })
			result = errors.Join(result, ErrCredentials)
			continue
		}
		s.mu.Lock()
		for j := range s.cleanup {
			if s.cleanup[j].credentials.Management.Token == management {
				s.cleanup[j].sessionDone = true
				s.cleanup[j].sessionRetained = false
				s.cleanup[j].failure = false
			}
		}
		s.mu.Unlock()
	}
	return result
}

func (s *Service) updateCleanup(id string, update func(*cleanupRecord)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cleanup {
		if s.cleanup[i].id == id {
			update(&s.cleanup[i])
			return
		}
	}
}

func (s *Service) cleanupViews() []Cleanup {
	result := make([]Cleanup, 0, len(s.cleanup))
	for _, r := range s.cleanup {
		value := Cleanup{ID: r.id, ExpiresAt: r.expires, TeamID: r.credentials.Scope.TeamID, KeyID: r.credentials.MachineKey.ID, KeyState: "pending", SessionState: "pending"}
		if r.credentials.MachineKey.ID == "" {
			value.KeyState = "absent"
		} else if r.keyDone {
			value.KeyState = "archived"
		}
		if r.credentials.Management.Token == "" {
			value.SessionState = "absent"
		} else if r.sessionDone {
			value.SessionState = "signed_out"
		} else if r.sessionRetained {
			value.SessionState = "retained"
		}
		if r.failure {
			value.Failure = "Remote cleanup remains unconfirmed"
		}
		result = append(result, value)
	}
	return result
}

func (s *Service) ListCleanup(ctx context.Context) ([]Cleanup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	s.prune()
	return s.cleanupViews(), nil
}

// RetryCleanup retries only captured old authority. It leaves current local
// credentials untouched and never borrows a newer account's management token.
func (s *Service) RetryCleanup(ctx context.Context) ([]Cleanup, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.prune()
	if s.active() != nil {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	current, _, err := s.capture()
	if err != nil {
		s.mu.Unlock()
		return nil, ErrCredentials
	}
	for _, r := range s.cleanup {
		if !r.complete() && (r.credentials.Management.Token != "" && r.credentials.Management.Token == current.Management.Token || r.credentials.MachineKey.ID != "" && r.credentials.MachineKey.ID == current.MachineKey.ID && r.credentials.Scope.TeamID == current.Scope.TeamID) {
			s.mu.Unlock()
			return nil, ErrBusy
		}
	}
	s.loggingOut = true
	s.wg.Add(1)
	defer s.wg.Done()
	s.mu.Unlock()
	remoteCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	s.control.Lock()
	err = s.remoteCleanup(remoteCtx)
	s.control.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loggingOut = false
	return s.cleanupViews(), err
}
