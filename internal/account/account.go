// Package account owns host-only ChatGPT onboarding flows. The command supplies
// its existing credential manager and config authority; account operations are
// ephemeral host operations, never session tools or durable execution receipts.
package account

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

var (
	ErrClosed        = errors.New("account service is closed")
	ErrNotFound      = errors.New("login flow is unavailable; start a new login")
	ErrInvalid       = errors.New("invalid account operation")
	ErrLimit         = errors.New("login flow limit reached; wait for retained flows to expire")
	ErrCredentials   = errors.New("saved OpenAI credentials need attention; inspect account status")
	ErrSetupRequired = errors.New("signed in to OpenAI, but host route setup needs attention; retry setup after correcting the configuration")
	ErrConfiguration = errors.New("OpenAI host route configuration needs attention")
	ErrLogout        = errors.New("OpenAI logout is not durable; correct credential storage and retry logout")
)

type FlowState string

const (
	Authorizing   FlowState = "authorizing"
	Succeeded     FlowState = "succeeded"
	SetupRequired FlowState = "setup_required"
	Failed        FlowState = "failed"
	Cancelled     FlowState = "cancelled"
	Expired       FlowState = "expired"
	Interrupted   FlowState = "interrupted"
	maxFlows                = 64
	retention               = 15 * time.Minute
)

// Flow contains only public approval details. ExpiresAt is zero when an old
// process identity is reported as interrupted without retaining its old record.
type Flow struct {
	ID              string    `json:"id"`
	State           FlowState `json:"state"`
	VerificationURL string    `json:"verification_url"`
	UserCode        string    `json:"user_code"`
	ExpiresAt       time.Time `json:"expires_at"`
	Failure         string    `json:"failure"`
}

// Status describes local evidence, not verified connectivity or model access.
// Stored credentials may be expired; inspecting them never triggers refresh.
type Status struct {
	AuthState  string     `json:"auth_state"`  // signed_out, stored, sign_in_required, unavailable
	RouteState string     `json:"route_state"` // configured, missing, conflict, unavailable
	AccountID  string     `json:"account_id"`
	Email      string     `json:"email"`
	Plan       string     `json:"plan"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Failure    string     `json:"failure"`
}

type login struct {
	view        Flow
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	retainUntil time.Time
}

// Service borrows the command's sole Manager and Authority. Close cancels and
// joins every owned device request before the command closes the Manager.
type Service struct {
	ctx      context.Context
	cancel   context.CancelFunc
	auth     *openaiauth.Manager
	config   *config.Authority
	epoch    string
	lifetime time.Duration
	mu       sync.Mutex // orders mutations and publication; never held during network requests
	wg       sync.WaitGroup
	closed   bool
	flows    map[string]*login
}

func New(ctx context.Context, auth *openaiauth.Manager, configuration *config.Authority) (*Service, error) {
	if auth == nil || configuration == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Service{
		ctx: ctx, cancel: cancel, auth: auth, config: configuration, epoch: rand.Text(),
		lifetime: openaiauth.DeviceLifetime, flows: map[string]*login{},
	}, nil
}

// Begin accepts one service-owned flow before issuing a device request. Caller
// disconnection/cancellation after acceptance does not cancel it; List and Get
// recover a lost acknowledgement, and concurrent Begin calls reuse that flow.
func (s *Service) Begin(ctx context.Context) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	for _, flow := range s.flows {
		if flow.view.State == Authorizing {
			return flow.view, nil
		}
	}
	if len(s.flows) == maxFlows {
		return Flow{}, ErrLimit
	}
	// A malformed local credential file cannot accept a completed login. Known
	// terminal rejection of an existing credential still permits reauthorization.
	credentials, err := s.auth.Snapshot()
	if err != nil && (credentials.AccessToken == "" || errors.Is(err, openaiauth.ErrPersistence)) {
		return Flow{}, ErrCredentials
	}
	configuration, err := s.config.Snapshot(ctx)
	if err != nil || configuration.Host.EnsureSubscription() != nil {
		return Flow{}, ErrConfiguration
	}
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	flowCtx, cancel := context.WithTimeout(s.ctx, s.lifetime)
	expires, _ := flowCtx.Deadline()
	flow := &login{
		view: Flow{ID: s.epoch + ":" + rand.Text(), State: Authorizing, ExpiresAt: expires},
		ctx:  flowCtx, cancel: cancel, done: make(chan struct{}),
	}
	s.flows[flow.view.ID] = flow
	generation := s.auth.Generation()
	s.wg.Add(1)
	go s.authorize(flow, generation)
	return flow.view, nil
}

func (s *Service) authorize(flow *login, generation uint64) {
	defer s.wg.Done()
	defer close(flow.done)
	defer flow.cancel()
	code, err := s.auth.StartDevice(flow.ctx)
	if err == nil {
		s.mu.Lock()
		if flow.view.State == Authorizing && flow.ctx.Err() == nil {
			flow.view.VerificationURL, flow.view.UserCode = code.VerificationURL, code.UserCode
		}
		s.mu.Unlock()
	}
	var credentials openaiauth.Credentials
	if err == nil {
		credentials, err = s.auth.CompleteDevice(flow.ctx, code)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow.view.State != Authorizing {
		return
	}
	if flow.ctx.Err() != nil {
		state := Interrupted
		if errors.Is(flow.ctx.Err(), context.DeadlineExceeded) {
			state = Expired
		}
		s.finish(flow, state, "")
		return
	}
	if err != nil {
		// The independent auth leaf intentionally returns bounded diagnostics
		// without response bodies, tokens, device IDs or authorization verifiers.
		s.finish(flow, Failed, err.Error())
		return
	}
	if err := s.auth.Install(flow.ctx, generation, credentials); err != nil {
		s.finish(flow, Failed, "OpenAI login could not be saved or was replaced; inspect account status before retrying")
		return
	}
	if err := s.setup(flow.ctx); err != nil {
		s.finish(flow, SetupRequired, ErrSetupRequired.Error())
		return
	}
	s.finish(flow, Succeeded, "")
}

func (s *Service) Get(ctx context.Context, id string) (Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Flow{}, err
	}
	s.prune()
	if flow := s.flows[id]; flow != nil {
		return flow.view, nil
	}
	epoch, suffix, ok := strings.Cut(id, ":")
	if !ok || !validID(epoch) || !validID(suffix) {
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
	for _, flow := range s.flows {
		result = append(result, flow.view)
	}
	slices.SortFunc(result, func(a, b Flow) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

// Cancel orders cancellation before any later credential installation and joins
// that flow's current device request. Terminal outcomes remain unchanged.
func (s *Service) Cancel(ctx context.Context, id string) (Flow, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return Flow{}, err
	}
	s.prune()
	flow := s.flows[id]
	if flow == nil {
		s.mu.Unlock()
		return Flow{}, ErrNotFound
	}
	if flow.view.State == Authorizing {
		s.finish(flow, Cancelled, "")
		flow.cancel()
	}
	result := flow.view
	s.mu.Unlock()
	<-flow.done
	return result, nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Status{}, err
	}
	return s.status(ctx), nil
}

// Setup confirms pending local credential persistence, then retries route
// publication. It never refreshes tokens or changes model defaults.
func (s *Service) Setup(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return Status{}, err
	}
	if err := s.auth.PersistPending(ctx); err != nil {
		return s.status(ctx), ErrCredentials
	}
	if err := s.setup(ctx); err != nil {
		return s.status(ctx), ErrSetupRequired
	}
	return s.status(ctx), nil
}

func (s *Service) setup(ctx context.Context) error {
	current, err := s.config.Snapshot(ctx)
	if err != nil {
		return err
	}
	_, err = s.config.Update(ctx, current.Revision, (*config.Host).EnsureSubscription)
	return err
}

func (s *Service) Logout(ctx context.Context) (Status, error) {
	return s.logout(ctx, func(clear func() error) error { return clear() })
}

// LogoutProvider orders the revision/shared-source check and local revocation
// under the existing login lock, so an older flow cannot publish afterward.
func (s *Service) LogoutProvider(ctx context.Context, revision, id string) (Status, config.ProviderDisconnect, error) {
	var result config.ProviderDisconnect
	status, err := s.logout(ctx, func(clear func() error) error {
		var err error
		result, err = s.config.DisconnectProvider(ctx, revision, id, "openai-codex", clear)
		return err
	})
	return status, result, err
}

func (s *Service) logout(ctx context.Context, guard func(func() error) error) (Status, error) {
	s.mu.Lock()
	if err := s.check(ctx); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	done := make([]<-chan struct{}, 0, len(s.flows))
	err := guard(func() error {
		for _, flow := range s.flows {
			if flow.view.State == Authorizing {
				s.finish(flow, Interrupted, "")
				flow.cancel()
			}
			done = append(done, flow.done)
		}
		return s.auth.Logout()
	})
	result := s.status(ctx)
	s.mu.Unlock()
	for _, finished := range done {
		<-finished
	}
	if err != nil {
		if errors.Is(err, config.ErrRevisionConflict) || errors.Is(err, session.ErrInvalid) {
			return result, err
		}
		return result, ErrLogout
	}
	return result, nil
}

func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	for _, flow := range s.flows {
		if flow.view.State == Authorizing {
			s.finish(flow, Interrupted, "")
			flow.cancel()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrClosed
	}
	return s.ctx.Err()
}

func (s *Service) finish(flow *login, state FlowState, failure string) {
	flow.view.State, flow.view.Failure = state, failure
	flow.view.VerificationURL, flow.view.UserCode = "", ""
	flow.retainUntil = time.Now().Add(retention)
}

func (s *Service) prune() {
	for id, flow := range s.flows {
		if flow.view.State != Authorizing && time.Now().After(flow.retainUntil) {
			delete(s.flows, id)
		}
	}
}

func validID(value string) bool {
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

func (s *Service) status(ctx context.Context) Status {
	result := Status{AuthState: "signed_out", RouteState: "unavailable"}
	credentials, err := s.auth.Snapshot()
	if credentials.AccessToken != "" {
		result.AuthState = "stored"
		result.AccountID, result.Email, result.Plan = metadata(credentials.AccountID, 512), metadata(credentials.Email, 320), metadata(credentials.Plan, 128)
		result.ExpiresAt = new(credentials.ExpiresAt)
	}
	if err != nil {
		result.AuthState = "unavailable"
		if credentials.AccessToken != "" && !errors.Is(err, openaiauth.ErrPersistence) {
			result.AuthState = "sign_in_required"
		}
		result.Failure = ErrCredentials.Error()
	}
	current, err := s.config.Snapshot(ctx)
	if err != nil {
		result.Failure = ErrConfiguration.Error()
		return result
	}
	route, exists := current.Host.Providers[openaiauth.Provider]
	result.RouteState = "missing"
	if exists {
		result.RouteState = "configured"
		if route.Kind != openaiauth.Provider {
			result.RouteState, result.Failure = "conflict", ErrConfiguration.Error()
		}
	}
	return result
}

func metadata(value string, limit int) string {
	if len(value) > limit || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return ""
	}
	return value
}
