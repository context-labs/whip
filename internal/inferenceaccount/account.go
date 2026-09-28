// Package inferenceaccount owns bounded host account operations. It borrows the
// command's credential manager; runtime sessions, SQL and provider execution are
// deliberately absent. Remote creation is never automatically retried.
package inferenceaccount

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/inferenceauth"
)

var (
	ErrInvalid     = errors.New("invalid Inference.net account operation")
	ErrClosed      = errors.New("inference.net account service is closed")
	ErrNotFound    = errors.New("inference.net account flow is unavailable")
	ErrBusy        = errors.New("another Inference.net account operation is active")
	ErrLimit       = errors.New("inference.net account flow limit reached")
	ErrCredentials = errors.New("inference.net credentials need attention; inspect local account status")
	ErrManagement  = errors.New("an unexpired Inference.net management session is required")
	ErrSetup       = errors.New("inference.net credentials are saved; retry host route setup")
)

type State string

const (
	Authorizing         State = "authorizing"
	ChooseTeam          State = "choose_team"
	LoadingProjects     State = "loading_projects"
	ChooseProject       State = "choose_project"
	CreatingProject     State = "creating_project"
	Provisioning        State = "provisioning"
	PersistenceRequired State = "persistence_required"
	SetupRequired       State = "setup_required"
	CleanupRequired     State = "cleanup_required"
	Succeeded           State = "succeeded"
	Failed              State = "failed"
	Uncertain           State = "uncertain"
	Cancelled           State = "cancelled"
	Expired             State = "expired"
	Interrupted         State = "interrupted"
	maxFlows                  = 64
	retention                 = 15 * time.Minute
)

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Flow is a safe bounded projection. Approval details are cleared after device
// approval or termination; credentials and device tokens never enter this type.
type Flow struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	State           State     `json:"state"`
	VerificationURL string    `json:"verification_url"`
	UserCode        string    `json:"user_code"`
	ExpiresAt       time.Time `json:"expires_at"`
	Teams           []Team    `json:"teams"`
	Projects        []Project `json:"projects"`
	TeamID          string    `json:"team_id"`
	ProjectID       string    `json:"project_id"`
	Failure         string    `json:"failure"`
}

type Status struct {
	ManagementState string     `json:"management_state"`
	InferenceState  string     `json:"inference_state"`
	UserID          string     `json:"user_id"`
	Email           string     `json:"email"`
	ExpiresAt       *time.Time `json:"expires_at"`
	TeamID          string     `json:"team_id"`
	TeamName        string     `json:"team_name"`
	ProjectID       string     `json:"project_id"`
	ProjectName     string     `json:"project_name"`
	Failure         string     `json:"failure"`
	CleanupPending  bool       `json:"cleanup_pending"`
}

type LogoutResult struct {
	Status         Status    `json:"status"`
	LocalFailure   string    `json:"local_failure"`
	CleanupFailure string    `json:"cleanup_failure"`
	Cleanup        []Cleanup `json:"cleanup"`
}

type flow struct {
	view        Flow
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	busy        bool
	creating    bool
	retainUntil time.Time
	generation  uint64
	credentials inferenceauth.Credentials
	previous    inferenceauth.Credentials
	pending     bool
	published   bool
	resume      string
}

// Service owns at most one active flow and serializes all set-active/dependent
// control-plane requests. setup publishes only the fixed nonsecret host route;
// it must not provision credentials, change defaults, or call remote services.
type Service struct {
	ctx           context.Context
	cancel        context.CancelFunc
	auth          *inferenceauth.Manager
	http          *http.Client
	setup         func(context.Context) error
	epoch         string
	lifetime      time.Duration
	wait          func(context.Context, time.Duration) error
	mu            sync.Mutex
	control       sync.Mutex
	wg            sync.WaitGroup
	closed        bool
	loggingOut    bool
	logoutPending bool
	cleanup       []cleanupRecord
	flows         map[string]*flow
}

func New(ctx context.Context, auth *inferenceauth.Manager, client *http.Client, setup func(context.Context) error) (*Service, error) {
	if auth == nil || setup == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	httpClient := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		httpClient = *client
	}
	if httpClient.Timeout <= 0 || httpClient.Timeout > 30*time.Second {
		httpClient.Timeout = 30 * time.Second
	}
	httpClient.Jar = nil // Management authorization is explicit, never ambient cookies.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithCancel(ctx)
	return &Service{ctx: ctx, cancel: cancel, auth: auth, http: &httpClient, setup: setup, epoch: rand.Text(), lifetime: retention, wait: sleep, flows: map[string]*flow{}}, nil
}

// Begin accepts host-owned work before HTTP and deduplicates the current flow.
// Caller disconnect after acceptance does not cancel the operation.
func (s *Service) Begin(ctx context.Context) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	if current := s.active(); current != nil {
		if current.view.Kind == "login" {
			return clone(current.view), nil
		}
		return Flow{}, ErrBusy
	}
	if len(s.cleanup) > maxFlows-3 {
		return Flow{}, ErrLimit
	}
	credentials, generation, err := s.capture()
	if err != nil {
		return Flow{}, ErrCredentials
	}
	f, err := s.newFlow("login", credentials, generation)
	if err != nil {
		return Flow{}, err
	}
	s.start(f, Authorizing, s.authorize)
	return clone(f.view), nil
}

// Rotate accepts a fresh, explicit key rotation under the saved project. An
// active rotation is recovered; terminal results are inspectable via Get/List.
func (s *Service) Rotate(ctx context.Context) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	if current := s.active(); current != nil {
		if current.view.Kind == "rotation" {
			return clone(current.view), nil
		}
		return Flow{}, ErrBusy
	}
	if len(s.cleanup) > maxFlows-3 {
		return Flow{}, ErrLimit
	}
	credentials, generation, err := s.capture()
	if err != nil {
		return Flow{}, ErrCredentials
	}
	if !managementAvailable(credentials) || credentials.MachineKey.Value == "" {
		return Flow{}, ErrManagement
	}
	f, err := s.newFlow("rotation", credentials, generation)
	if err != nil {
		return Flow{}, err
	}
	f.view.TeamID, f.view.ProjectID = credentials.Scope.TeamID, credentials.Scope.ProjectID
	f.view.Teams = []Team{{ID: credentials.Scope.TeamID, Name: credentials.Scope.TeamName}}
	f.view.Projects = []Project{{ID: credentials.Scope.ProjectID, Name: credentials.Scope.ProjectName}}
	s.start(f, Provisioning, s.provision)
	return clone(f.view), nil
}

func (s *Service) newFlow(kind string, credentials inferenceauth.Credentials, generation uint64) (*flow, error) {
	if len(s.flows) >= maxFlows {
		return nil, ErrLimit
	}
	ctx, cancel := context.WithTimeout(s.ctx, s.lifetime)
	expires, _ := ctx.Deadline()
	done := make(chan struct{})
	close(done)
	f := &flow{view: Flow{ID: s.epoch + ":" + rand.Text(), Kind: kind, State: Authorizing, ExpiresAt: expires}, ctx: ctx, cancel: cancel, done: done, generation: generation, credentials: credentials, previous: credentials}
	s.flows[f.view.ID] = f
	return f, nil
}

// start requires mu. The control lock protects the remote management session's
// active organization, while mu remains free for cancellation and observation.
func (s *Service) start(f *flow, state State, work func(*flow) error) {
	f.view.State, f.view.Failure, f.busy, f.done = state, "", true, make(chan struct{})
	s.wg.Go(func() {
		s.control.Lock()
		err := s.guard(f)
		if err == nil {
			err = work(f)
		}
		s.control.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !terminal(f.view.State) && err != nil {
			switch {
			case uncertain(err):
				s.finish(f, Uncertain, err.Error())
			case f.ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
				state := Interrupted
				if errors.Is(f.ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
					state = Expired
				}
				s.finish(f, state, "")
			case errors.Is(err, inferenceauth.ErrChanged):
				s.finish(f, Interrupted, "Account authorization changed")
			case f.view.State == PersistenceRequired:
				// The known local publication remains explicitly retryable.
			default:
				s.finish(f, Failed, "Inference.net account operation failed")
			}
		}
		f.busy = false
		if terminal(f.view.State) {
			f.credentials, f.previous = inferenceauth.Credentials{}, inferenceauth.Credentials{}
			f.view.Teams, f.view.Projects = nil, nil
		}
		close(f.done)
	})
}

func (s *Service) guard(f *flow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(f.ctx); err != nil {
		return err
	}
	if terminal(f.view.State) {
		return context.Canceled
	}
	if s.auth.Generation() != f.generation {
		return inferenceauth.ErrChanged
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	if f := s.flows[id]; f != nil {
		return clone(f.view), nil
	}
	epoch, suffix, ok := strings.Cut(id, ":")
	if !ok || !validFlowID(epoch) || !validFlowID(suffix) {
		return Flow{}, ErrInvalid
	}
	if epoch != s.epoch {
		return Flow{ID: id, State: Interrupted}, nil
	}
	return Flow{}, ErrNotFound
}

func (s *Service) List(ctx context.Context) ([]Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	s.prune()
	result := make([]Flow, 0, len(s.flows))
	for _, f := range s.flows {
		result = append(result, clone(f.view))
	}
	slices.SortFunc(result, func(a, b Flow) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, id string) (Flow, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return Flow{}, err
	}
	f := s.flows[id]
	if f == nil {
		s.mu.Unlock()
		return Flow{}, ErrNotFound
	}
	if !terminal(f.view.State) {
		s.finish(f, Cancelled, "")
		f.cancel()
	}
	done := f.done
	s.mu.Unlock()
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	f.credentials, f.previous = inferenceauth.Credentials{}, inferenceauth.Credentials{}
	return clone(f.view), nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Status{}, err
	}
	s.prune()
	return s.status(), nil
}

func (s *Service) status() Status {
	value, _, err := s.capture()
	result := Status{ManagementState: "absent", InferenceState: "absent"}
	for _, record := range s.cleanup {
		result.CleanupPending = result.CleanupPending || !record.complete()
	}
	if err != nil {
		result.ManagementState, result.InferenceState, result.Failure = "unavailable", "unavailable", ErrCredentials.Error()
		return result
	}
	if value.Management.Token != "" {
		result.ManagementState = "stored"
		if !managementAvailable(value) {
			result.ManagementState = "expired"
		}
		result.UserID, result.Email, result.ExpiresAt = value.Management.UserID, value.Management.Email, value.Management.ExpiresAt
	}
	if value.MachineKey.Value != "" {
		result.InferenceState = "stored"
	}
	result.TeamID, result.TeamName, result.ProjectID, result.ProjectName = value.Scope.TeamID, value.Scope.TeamName, value.Scope.ProjectID, value.Scope.ProjectName
	return result
}

// Setup performs only local persistence/route repair. It cannot complete a
// pending logout or repeat a device exchange, project creation or key mint.
func (s *Service) Setup(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Status{}, err
	}
	s.prune()
	if s.active() != nil {
		return s.status(), ErrBusy
	}
	if _, err := s.auth.Capture(ctx); err != nil {
		return s.status(), ErrCredentials
	}
	if err := s.setup(ctx); err != nil {
		return s.status(), ErrSetup
	}
	return s.status(), nil
}

func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	for _, f := range s.flows {
		if !terminal(f.view.State) {
			s.finish(f, Interrupted, "")
			f.cancel()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.flows {
		f.credentials, f.previous = inferenceauth.Credentials{}, inferenceauth.Credentials{}
	}
	s.cleanup = nil
}

func (s *Service) capture() (inferenceauth.Credentials, uint64, error) {
	generation := s.auth.Generation()
	value, err := s.auth.Snapshot()
	if s.auth.Generation() != generation {
		return inferenceauth.Credentials{}, 0, inferenceauth.ErrChanged
	}
	return value, generation, err
}

func (s *Service) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrClosed
	}
	if s.loggingOut {
		return ErrBusy
	}
	return s.ctx.Err()
}

func (s *Service) active() *flow {
	for _, f := range s.flows {
		if !terminal(f.view.State) {
			return f
		}
	}
	return nil
}

func (s *Service) finish(f *flow, state State, failure string) {
	if f.creating && (state == Cancelled || state == Interrupted || state == Expired) {
		state, failure = Uncertain, "Remote creation was dispatched; inspect the account before starting another operation"
	}
	if terminal(state) && state != Succeeded {
		// A cancelled/expired local retry must not erase known remote effects.
		// Admission reserves room for the prior and proposed credentials.
		_ = s.addCleanup(f.previous)
		if f.pending {
			_ = s.addCleanup(f.credentials)
		}
	}
	f.view.State, f.view.Failure = state, failure
	f.view.VerificationURL, f.view.UserCode = "", ""
	f.retainUntil = time.Now().Add(retention)
	if terminal(state) {
		f.cancel()
	}
}

func (s *Service) prune() {
	if !s.logoutPending {
		s.cleanup = slices.DeleteFunc(s.cleanup, func(record cleanupRecord) bool { return time.Now().After(record.expires) })
	}
	for id, f := range s.flows {
		if !terminal(f.view.State) && !f.busy && f.ctx.Err() != nil {
			s.finish(f, Expired, "")
			f.credentials, f.previous = inferenceauth.Credentials{}, inferenceauth.Credentials{}
		}
		if terminal(f.view.State) && !f.busy && time.Now().After(f.retainUntil) {
			delete(s.flows, id)
		}
	}
}

func terminal(state State) bool {
	return state == Succeeded || state == Failed || state == Uncertain || state == Cancelled || state == Expired || state == Interrupted
}

func clone(f Flow) Flow {
	if terminal(f.State) {
		f.Teams, f.Projects = nil, nil
		return f
	}
	f.Teams = slices.Clone(f.Teams)
	f.Projects = slices.Clone(f.Projects)
	return f
}

func validFlowID(value string) bool {
	if len(value) != 26 {
		return false
	}
	for _, c := range value {
		if (c < 'A' || c > 'Z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

func text(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func token(value string) bool {
	if len(value) == 0 || len(value) > 16<<10 {
		return false
	}
	for _, c := range []byte(value) {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func managementAvailable(value inferenceauth.Credentials) bool {
	return value.Management.Token != "" && (value.Management.ExpiresAt == nil || value.Management.ExpiresAt.After(time.Now()))
}
