package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) browserIdentity(ctx context.Context, current session.Session) (browserhost.Identity, error) {
	root, err := r.store.Root(ctx, current.TreeID)
	return browserhost.Identity{RootID: string(root.ID), AgentID: string(current.ID)}, err
}

func (r *Runtime) BrowserAttachments(ctx context.Context, id session.SessionID) ([]browserhost.Attachment, error) {
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	identity, err := r.browserIdentity(ctx, owner)
	if err != nil {
		return nil, err
	}
	return r.browser.Attachments(identity), nil
}

// BrowserTabs performs explicit bounded discovery against a human offer. It
// never selects an unoffered tab or turns returned metadata into control.
func (r *Runtime) BrowserTabs(ctx context.Context, id session.SessionID) ([]browserhost.Tab, error) {
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	identity, err := r.browserIdentity(ctx, owner)
	if err != nil {
		return nil, err
	}
	return r.browser.ListTabs(ctx, identity)
}

// BrowserResult keeps the retained flat helper result while scoped image refs
// use the canonical v4 content owner. Metadata is never itself control authority.
type BrowserResult struct {
	Tabs                []browserhost.Tab          `json:"tabs"`
	Availability        string                     `json:"availability"`
	AttachmentID        string                     `json:"attachment_id"`
	TabID               string                     `json:"tab_id"`
	DocumentRevision    string                     `json:"document_revision"`
	URL                 string                     `json:"url"`
	Title               string                     `json:"title"`
	Network             BrowserNetwork             `json:"network"`
	SupportedOperations []string                   `json:"supported_operations"`
	Output              string                     `json:"output"`
	Screenshots         []session.ContentReference `json:"screenshots"`
}
type BrowserNetwork struct {
	Kind   string `json:"kind"`
	HostID string `json:"host_id"`
	Ports  []int  `json:"ports"`
}

func browserResult(attached browserhost.Attachment) BrowserResult {
	result := BrowserResult{Tabs: []browserhost.Tab{}, Availability: "available", AttachmentID: attached.Scope.AttachmentID, TabID: attached.Scope.TabID, DocumentRevision: attached.DocumentRevision, URL: attached.URL, Title: attached.Title, Network: BrowserNetwork{Kind: "mac", Ports: []int{}}, SupportedOperations: []string{"run", "detach"}, Screenshots: []session.ContentReference{}}
	if attached.Scope.AttachmentID == "" {
		result.Availability = "unavailable"
		result.Network.Kind = "unknown"
		result.SupportedOperations = []string{}
	}
	if p := attached.Scope.Preview; p != nil {
		result.Network = BrowserNetwork{Kind: "preview", HostID: p.HostID, Ports: slices.Clone(p.Ports)}
		if attached.Owner.RootID == attached.Owner.AgentID {
			result.SupportedOperations = append(result.SupportedOperations, "allow_preview_port")
		}
	}
	return result
}

type browserArguments struct {
	browserhost.Arguments
	Code    string  `json:"code,omitempty"`
	Timeout float64 `json:"timeout,omitempty"`
}

func (r *Runtime) prepareBrowser(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	owner, err := r.browserIdentity(ctx, current)
	if err != nil {
		return tool.Prepared{}, err
	}
	if call.Name == "list_tabs" {
		var args struct{}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return tool.Prepared{}, session.ErrInvalid
		}
		raw, _ := json.Marshal(session.BrowserCatalogRequest{SessionID: current.ID, TreeID: current.TreeID, ConfigRevision: current.ConfigRevision, Action: "list_tabs"})
		return tool.Prepared{Capability: "browser.catalog", Resource: string(current.TreeID), Arguments: raw, Timeout: 10 * time.Second, Acquire: func(context.Context) (func(), error) { return func() {}, nil }, Run: func(ctx context.Context, id session.OperationID) (any, error) {
			if err := r.store.CheckDispatchedOperation(ctx, id); err != nil {
				return nil, err
			}
			tabs, err := r.browser.ListTabs(ctx, owner)
			result := browserResult(browserhost.Attachment{})
			result.Tabs = tabs
			if err == nil {
				result.Availability = "available"
			}
			result.SupportedOperations = []string{"list_tabs", "open", "attach"}
			return result, err
		}}, nil
	}
	var args browserArguments
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, session.ErrInvalid
	}
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) || args.Timeout < 0 || args.Timeout > 120 || (args.Timeout > 0 && args.Timeout < 0.001) {
		return tool.Prepared{}, session.ErrInvalid
	}
	if call.Name == "open" {
		if err := browser.ValidateNavigationURL(args.URL); err != nil {
			return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
		}
	}
	var program *browser.Program
	if call.Name == "run" {
		program, err = browser.CompileProgram(args.Code)
		if err != nil {
			return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
		}
	} else if args.Code != "" || args.Timeout != 0 {
		return tool.Prepared{}, session.ErrInvalid
	}
	capture, err := r.browser.Resolve(ctx, owner, call.Name, args.Arguments)
	if err != nil {
		return tool.Prepared{}, err
	}
	scope := capture.Scope()
	if program != nil {
		if err := program.ValidateTarget(scope.TabID); err != nil {
			return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
		}
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		return tool.Prepared{}, err
	}
	intent := session.BrowserIntent{SessionID: current.ID, TreeID: current.TreeID, ConfigRevision: current.ConfigRevision, Kind: call.Name, Scope: scope, PreviousResource: capture.PreviousResource(), Arguments: arguments}
	intent.TargetURL, intent.TargetTitle, intent.TargetDocument = capture.Target()
	if err := intent.Validate(); err != nil {
		return tool.Prepared{}, err
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return tool.Prepared{}, err
	}
	timeout := 60 * time.Second
	if args.Timeout != 0 {
		timeout = time.Duration(args.Timeout * float64(time.Second))
	}
	var lease *browserhost.Lease
	var images *hostImages
	return tool.Prepared{
		Capability: "browser.control", Resource: scope.Resource(), Arguments: raw, Mutating: true, Lifetime: capture.Lifetime(), Timeout: timeout,
		Acquire: func(ctx context.Context) (func(), error) {
			releaseImages := func() {}
			var err error
			if program != nil && program.Screenshots() > 0 {
				images, releaseImages, err = r.acquireHostImages(ctx, current, call)
				if err != nil {
					return nil, err
				}
			}
			lease, err = capture.Acquire(ctx)
			if err != nil {
				releaseImages()
				return nil, err
			}
			return func() { lease.Close(); releaseImages() }, nil
		},
		Run: func(ctx context.Context, id session.OperationID) (any, error) {
			check := func(ctx context.Context) error { return r.store.CheckDispatchedOperation(ctx, id) }
			if program == nil {
				if call.Name == "open" {
					if err := browser.CheckURL(ctx, args.URL); err != nil {
						return nil, tool.SettledFailure(err)
					}
				}
				result, err := lease.Execute(ctx, string(id), check, func(ctx context.Context, attached browserhost.Attachment) error {
					if attached.Owner != owner || attached.Scope.Resource() != scope.Resource() {
						return store.ErrConflict
					}
					return r.store.PublishBrowserControl(ctx, id)
				})
				value := browserResult(result)
				if err == nil && call.Name == "detach" {
					value.Availability = "detached"
					value.SupportedOperations = []string{}
				}
				return value, browserFailure(err)
			}
			refs := []session.ContentReference{}
			text := ""
			attached, runErr := lease.Run(ctx, string(id), check, func(ctx context.Context, batch *browserhost.Batch) error {
				client, err := batch.NewCDPClient()
				if err != nil {
					return err
				}
				backend, err := browser.NewDesktopBackend(ctx, client, scope.TabID, func() error { client.Close(); return nil })
				if err != nil {
					return err
				}
				defer func() { _ = backend.Close() }()
				limits := browser.ProgramLimits{}
				if images != nil {
					limits.Images, limits.ImageBytes = images.count, int(images.bytes)
				}
				text, err = program.Run(ctx, backend, limits, func(ctx context.Context, data []byte) error {
					publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					ref, err := r.publishHostImage(publishCtx, current.ID, id, len(refs), "image/jpeg", data, images)
					if err == nil {
						refs = append(refs, ref)
					}
					return err
				})
				return err
			})
			value := browserResult(attached)
			value.Output = text
			value.Screenshots = refs
			result := tool.Output{Value: value}
			if images != nil {
				result.ContentReferences = images.refs
			}
			return result, browserFailure(runErr)
		},
	}, nil
}

func browserFailure(err error) error {
	var failure *browserhost.Failure
	if errors.As(err, &failure) && !errors.Is(err, browserhost.ErrUnknown) && !errors.Is(err, browserhost.ErrStale) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return tool.SettledFailure(err)
	}
	return err
}

func (r *Runtime) cleanupBrowserOwners(ctx context.Context) error {
	for _, identity := range r.browser.Owners() {
		if _, err := r.store.Session(ctx, session.SessionID(identity.AgentID)); errors.Is(err, store.ErrNotFound) {
			if identity.AgentID == identity.RootID {
				r.browser.RevokeRoot(identity.RootID)
			} else {
				r.browser.RevokeOwner(identity)
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}
