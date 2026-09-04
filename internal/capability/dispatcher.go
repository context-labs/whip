package capability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrDenied         = errors.New("capability denied")
	ErrStaleAdmission = errors.New("capability admission changed")
)

type Mutation string

const (
	MutationNone      Mutation = "none"
	MutationPath      Mutation = "path"
	MutationWorkspace Mutation = "workspace"
)

type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusDenied    Status = "denied"
)

type Reservation struct {
	Kind    string `json:"kind"`
	Amount  int64  `json:"amount"`
	Consume bool   `json:"consume,omitempty"`
}

type Usage struct {
	Kind   string `json:"kind"`
	Amount int64  `json:"amount"`
}

type Grant struct {
	ID            string
	RootID        string
	AgentID       string
	IssuerAgentID string
	Operations    []string
	Scopes        []string
	Generation    int64
	ExpiresAt     time.Time
}

type Reference struct {
	ID         string
	Generation int64
}

// Authority is one agent's dispatcher identity and capability references.
type Authority struct {
	RootID  string
	AgentID string
	Files   Reference
	Shell   Reference
}

type Request struct {
	RootID                     string          `json:"root_id"`
	AgentID                    string          `json:"agent_id"`
	CapabilityID               string          `json:"capability_id"`
	CapabilityGeneration       int64           `json:"capability_generation"`
	WriterCapabilityID         string          `json:"writer_capability_id,omitempty"`
	WriterCapabilityGeneration int64           `json:"writer_capability_generation,omitempty"`
	OperationID                string          `json:"operation_id"`
	Operation                  string          `json:"operation"`
	Arguments                  json.RawMessage `json:"arguments"`
	CommandClientID            string          `json:"command_client_id,omitempty"`
	CommandID                  string          `json:"command_id,omitempty"`
	TraceID                    string          `json:"trace_id"`
	WorkingDirectory           string          `json:"working_directory,omitempty"`
	Reservations               []Reservation   `json:"reservations"`
}

type Admission struct {
	Request           Request  `json:"request"`
	CanonicalRoot     string   `json:"canonical_root"`
	CanonicalPath     string   `json:"canonical_path,omitempty"`
	Mutation          Mutation `json:"mutation"`
	RequirePermission bool     `json:"require_permission"`
	RequestDigest     string   `json:"request_digest"`
}

type Ticket struct {
	OperationID  string
	LeaseID      string
	PermissionID string
}

type Completion struct {
	Admission Admission
	LeaseID   string
	Status    Status
	Output    string
	Error     string
	Usage     []Usage
}

type Decision struct {
	Allow       bool
	PrincipalID string
	Reason      string
	Remember    string // "", "tree", or "global": install the prompt's rule at that scope
}

type PermissionPrompt struct {
	ID            string
	Operation     string
	Arguments     json.RawMessage
	Digest        string
	CanonicalPath string
}

type PermissionApprover interface {
	Decide(context.Context, PermissionPrompt) (Decision, error)
}

type PermissionPendingError struct {
	PermissionID string
	OperationID  string
}

func (e *PermissionPendingError) Error() string {
	return fmt.Sprintf("permission %s is pending for operation %s", e.PermissionID, e.OperationID)
}

type PermissionDeniedError struct{ Reason string }

func (e *PermissionDeniedError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "the user rejected this action"
	}
	return "Permission denied: " + reason
}

func (e *PermissionDeniedError) Unwrap() error { return ErrDenied }

type Ledger interface {
	WorkspaceRoot(context.Context, string) (string, error)
	Begin(context.Context, Admission) (Ticket, error)
	Pending(context.Context, string) (Admission, error)
	Decide(context.Context, Admission, string, Decision) (Ticket, error)
	Finish(context.Context, Completion) error
}

type Call struct {
	Request       Request
	Arguments     json.RawMessage
	CanonicalRoot string
	CanonicalPath string
	WorkingDir    string
}

type Registration struct {
	Operation  string
	Mutation   Mutation
	Permission bool
	Path       func(json.RawMessage) (string, error)
	Handler    func(context.Context, Call) (string, error)
}

type Response struct {
	OperationID string
	Output      string
}

type Dispatcher struct {
	ledger      Ledger
	workspaces  *Workspaces
	permissions PermissionApprover
	mu          sync.RWMutex
	operations  map[string]Registration
}

func NewDispatcher(ledger Ledger, workspaces *Workspaces, permissions PermissionApprover) *Dispatcher {
	return &Dispatcher{ledger: ledger, workspaces: workspaces, permissions: permissions, operations: make(map[string]Registration)}
}

func (d *Dispatcher) Register(registration Registration) error {
	if registration.Operation == "" || registration.Handler == nil {
		return errors.New("capability registration requires an operation and handler")
	}
	if registration.Mutation == "" {
		registration.Mutation = MutationNone
	}
	if registration.Mutation != MutationNone && registration.Mutation != MutationPath && registration.Mutation != MutationWorkspace {
		return fmt.Errorf("invalid mutation mode %q", registration.Mutation)
	}
	if registration.Mutation == MutationPath && registration.Path == nil {
		return errors.New("path mutation requires a path extractor")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.operations[registration.Operation]; exists {
		return fmt.Errorf("operation %q is already registered", registration.Operation)
	}
	d.operations[registration.Operation] = registration
	return nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, request Request) (Response, error) {
	registration, err := d.registration(request.Operation)
	if err != nil {
		return Response{}, err
	}
	admission, workspace, err := d.admission(ctx, request, registration)
	if err != nil {
		return Response{}, err
	}
	ticket, err := d.ledger.Begin(ctx, admission)
	if err != nil {
		return Response{}, err
	}
	if ticket.PermissionID != "" {
		if d.permissions == nil {
			return Response{}, &PermissionPendingError{PermissionID: ticket.PermissionID, OperationID: ticket.OperationID}
		}
		decision, err := d.permissions.Decide(ctx, PermissionPrompt{
			ID: ticket.PermissionID, Operation: request.Operation, Arguments: admission.Request.Arguments,
			Digest: admission.RequestDigest, CanonicalPath: admission.CanonicalPath,
		})
		if err != nil {
			_, cleanupErr := d.ledger.Decide(context.WithoutCancel(ctx), admission, ticket.PermissionID, Decision{
				PrincipalID: "permission-adapter", Reason: err.Error(),
			})
			if !errors.Is(cleanupErr, ErrDenied) {
				err = errors.Join(err, cleanupErr)
			}
			return Response{}, err
		}
		if err := ctx.Err(); err != nil {
			_, cleanupErr := d.ledger.Decide(context.WithoutCancel(ctx), admission, ticket.PermissionID, Decision{
				PrincipalID: decision.PrincipalID, Reason: err.Error(),
			})
			if !errors.Is(cleanupErr, ErrDenied) {
				err = errors.Join(err, cleanupErr)
			}
			return Response{}, err
		}
		return d.Decide(ctx, ticket.PermissionID, decision)
	}
	return d.execute(ctx, registration, admission, workspace, ticket)
}

func (d *Dispatcher) Decide(ctx context.Context, permissionID string, decision Decision) (Response, error) {
	if permissionID == "" || decision.PrincipalID == "" {
		return Response{}, errors.New("permission and principal IDs are required")
	}
	stored, err := d.ledger.Pending(ctx, permissionID)
	if err != nil {
		return Response{}, err
	}
	registration, err := d.registration(stored.Request.Operation)
	if err != nil {
		return Response{}, err
	}
	current, workspace, err := d.admission(ctx, stored.Request, registration)
	if err != nil {
		_, _ = d.ledger.Decide(context.WithoutCancel(ctx), stored, permissionID, Decision{PrincipalID: decision.PrincipalID, Reason: ErrStaleAdmission.Error()})
		return Response{}, err
	}
	if current.RequestDigest != stored.RequestDigest || current.CanonicalRoot != stored.CanonicalRoot || current.CanonicalPath != stored.CanonicalPath {
		_, _ = d.ledger.Decide(context.WithoutCancel(ctx), stored, permissionID, Decision{PrincipalID: decision.PrincipalID, Reason: ErrStaleAdmission.Error()})
		return Response{}, ErrStaleAdmission
	}
	ticket, err := d.ledger.Decide(ctx, current, permissionID, decision)
	if err != nil {
		if !decision.Allow && errors.Is(err, ErrDenied) {
			return Response{}, &PermissionDeniedError{Reason: decision.Reason}
		}
		return Response{}, err
	}
	if !decision.Allow {
		return Response{}, ErrDenied
	}
	return d.execute(ctx, registration, current, workspace, ticket)
}

func (d *Dispatcher) registration(operation string) (Registration, error) {
	d.mu.RLock()
	registration, ok := d.operations[operation]
	d.mu.RUnlock()
	if !ok {
		return Registration{}, fmt.Errorf("unknown capability operation %q", operation)
	}
	return registration, nil
}

func (d *Dispatcher) admission(ctx context.Context, request Request, registration Registration) (Admission, *Workspace, error) {
	if d.ledger == nil || d.workspaces == nil {
		return Admission{}, nil, errors.New("dispatcher requires a ledger and workspace authority")
	}
	if request.RootID == "" || request.AgentID == "" || request.CapabilityID == "" || request.OperationID == "" || request.TraceID == "" {
		return Admission{}, nil, errors.New("capability request identity is incomplete")
	}
	if (request.CommandClientID == "") != (request.CommandID == "") {
		return Admission{}, nil, errors.New("capability request command identity is incomplete")
	}
	arguments, err := canonicalJSON(request.Arguments)
	if err != nil {
		return Admission{}, nil, fmt.Errorf("invalid capability arguments: %w", err)
	}
	request.Arguments = arguments
	if len(request.Reservations) == 0 {
		request.Reservations = []Reservation{{Kind: "active_operations", Amount: 1}}
	}
	budgetKinds := make(map[string]struct{}, len(request.Reservations))
	for _, reservation := range request.Reservations {
		if reservation.Kind == "" || reservation.Amount <= 0 {
			return Admission{}, nil, errors.New("capability reservation must have a kind and positive amount")
		}
		if _, duplicate := budgetKinds[reservation.Kind]; duplicate {
			return Admission{}, nil, fmt.Errorf("duplicate capability reservation %q", reservation.Kind)
		}
		budgetKinds[reservation.Kind] = struct{}{}
	}
	root, err := d.ledger.WorkspaceRoot(ctx, request.RootID)
	if err != nil {
		return Admission{}, nil, err
	}
	workspace, err := d.workspaces.Open(root)
	if err != nil {
		return Admission{}, nil, err
	}
	admission := Admission{
		Request: request, CanonicalRoot: workspace.Root(), Mutation: registration.Mutation,
		RequirePermission: registration.Permission,
	}
	workingDir := workspace.Root()
	if request.WorkingDirectory != "" {
		workingDir, err = workspace.Resolve(request.WorkingDirectory)
		if err != nil {
			return Admission{}, nil, err
		}
		admission.Request.WorkingDirectory = workingDir
	}
	if registration.Path != nil {
		path, err := registration.Path(arguments)
		if err != nil {
			return Admission{}, nil, err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		admission.CanonicalPath, err = workspace.Resolve(path)
		if err != nil {
			return Admission{}, nil, err
		}
	}
	digestInput, err := json.Marshal(admission)
	if err != nil {
		return Admission{}, nil, err
	}
	sum := sha256.Sum256(digestInput)
	admission.RequestDigest = hex.EncodeToString(sum[:])
	return admission, workspace, nil
}

func (d *Dispatcher) execute(ctx context.Context, registration Registration, admission Admission, workspace *Workspace, ticket Ticket) (Response, error) {
	var release func()
	var err error
	// Only path mutations lock, and only against the same canonical path.
	// Workspace mutations (shell, processes) run concurrently: their effects
	// cannot be proven path-local, so serializing them would block every
	// read-only command in the tree for no gain. Their authority (a writer
	// capability scoped to the root) was checked at admission.
	if admission.Mutation == MutationPath {
		var canonicalPath string
		canonicalPath, release, err = workspace.LockPath(ctx, admission.CanonicalPath)
		if err == nil && canonicalPath != admission.CanonicalPath {
			err = ErrStaleAdmission
		}
	}
	if release != nil {
		defer release()
	}
	if err != nil {
		completion := Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: StatusFailed, Error: err.Error()}
		return Response{}, errors.Join(err, d.ledger.Finish(context.WithoutCancel(ctx), completion))
	}
	output, handlerErr := registration.Handler(ctx, Call{
		Request: admission.Request, Arguments: admission.Request.Arguments,
		CanonicalRoot: admission.CanonicalRoot, CanonicalPath: admission.CanonicalPath,
		WorkingDir: admission.Request.WorkingDirectory,
	})
	completion := Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: StatusSucceeded, Output: output}
	if handlerErr != nil {
		completion.Status = StatusFailed
		completion.Error = handlerErr.Error()
	}
	if finishErr := d.ledger.Finish(context.WithoutCancel(ctx), completion); finishErr != nil {
		return Response{}, errors.Join(handlerErr, finishErr)
	}
	return Response{OperationID: admission.Request.OperationID, Output: output}, handlerErr
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values")
	}
	return json.Marshal(value)
}
