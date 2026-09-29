package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

type ExternalBrowserStatus struct {
	Revision      string
	Configuration browserconfig.Config
	Driver        string
	DriverPinned  bool
}

// ExternalBrowserStatus reads declarations only. Updating live ownership here
// can retire obsolete generations, but never probes or launches a browser.
func (r *Runtime) ExternalBrowserStatus(ctx context.Context) (ExternalBrowserStatus, error) {
	r.externalBrowserMu.Lock()
	defer r.externalBrowserMu.Unlock()
	return r.externalBrowserStatus(ctx)
}

func (r *Runtime) externalBrowserStatus(ctx context.Context) (ExternalBrowserStatus, error) {
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return ExternalBrowserStatus{}, err
	}
	selection := r.browserDriver(snapshot)
	settings, err := snapshot.Host.ExternalBrowser.Normalize()
	if err != nil {
		return ExternalBrowserStatus{}, err
	}
	if err := r.externalBrowser.Update(settings, selection.Driver); err != nil {
		return ExternalBrowserStatus{}, err
	}
	return ExternalBrowserStatus{Revision: snapshot.Revision, Configuration: settings, Driver: selection.Driver, DriverPinned: selection.Pinned}, nil
}

func (r *Runtime) ConfigureExternalBrowser(ctx context.Context, revision string, settings browserconfig.Config) (ExternalBrowserStatus, error) {
	settings, err := settings.Normalize()
	if err != nil {
		return ExternalBrowserStatus{}, errors.Join(session.ErrInvalid, err)
	}
	r.externalBrowserMu.Lock()
	defer r.externalBrowserMu.Unlock()
	_, err = r.configuration.Update(ctx, revision, func(host *config.Host) error { host.ExternalBrowser = settings; return nil })
	// Publication may have succeeded before a durability error. Re-read and retire
	// obsolete authority even then, while preserving that error for the caller.
	result, refreshErr := r.externalBrowserStatus(context.WithoutCancel(ctx))
	return result, errors.Join(err, refreshErr)
}

func (r *Runtime) ExternalBrowserSessions(ctx context.Context, id session.SessionID) ([]browser.NativeDescription, error) {
	current, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	owner, err := r.browserIdentity(ctx, current)
	if err != nil {
		return nil, err
	}
	if _, err := r.ExternalBrowserStatus(ctx); err != nil {
		return nil, err
	}
	return r.externalBrowser.List(owner.RootID), nil
}

// ReconnectExternalBrowser is a root human control, not an agent action or a
// recovered checkpoint. It creates no connection; the next Run must be admitted.
func (r *Runtime) ChangeExternalBrowserConnection(ctx context.Context, id session.SessionID, name, generation string, reconnect bool) (browser.NativeDescription, error) {
	current, err := r.store.Session(ctx, id)
	if err != nil {
		return browser.NativeDescription{}, err
	}
	owner, err := r.browserIdentity(ctx, current)
	if err != nil {
		return browser.NativeDescription{}, err
	}
	if owner.RootID != owner.AgentID || current.Lifecycle != session.Active {
		return browser.NativeDescription{}, session.ErrInvalid
	}
	r.externalBrowserMu.Lock()
	defer r.externalBrowserMu.Unlock()
	if _, err := r.externalBrowserStatus(ctx); err != nil {
		return browser.NativeDescription{}, err
	}
	var value browser.NativeDescription
	if reconnect {
		value, err = r.externalBrowser.Reconnect(owner.RootID, name, generation)
	} else {
		value, err = r.externalBrowser.Disconnect(owner.RootID, name, generation)
	}
	if errors.Is(err, browser.ErrNativeStale) {
		err = store.ErrConflict
	}
	return value, err
}

type externalBrowserResult struct {
	Session      string                     `json:"session"`
	Mode         string                     `json:"mode"`
	Generation   string                     `json:"generation"`
	Availability string                     `json:"availability"`
	Output       string                     `json:"output"`
	Screenshots  []session.ContentReference `json:"screenshots"`
}

func (r *Runtime) prepareExternalBrowser(ctx context.Context, current session.Session, call tool.Invocation, args browserArguments) (tool.Prepared, error) {
	if args.Arguments != (browserhost.Arguments{}) || (call.Name != "run" && call.Name != "detach") || (call.Name == "detach" && (args.Code != "" || args.Timeout != 0)) {
		return tool.Prepared{}, session.ErrInvalid
	}
	owner, err := r.browserIdentity(ctx, current)
	if err != nil {
		return tool.Prepared{}, err
	}
	status, err := r.ExternalBrowserStatus(ctx)
	if err != nil {
		return tool.Prepared{}, err
	}
	var program *browser.Program
	if call.Name == "run" {
		program, err = browser.CompileNativeProgram(args.Code, status.Configuration.Mode == "live", status.Configuration.AllowPrivateURLs)
		if err != nil {
			return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
		}
	}
	capture, err := r.externalBrowser.Capture(owner.RootID, owner.AgentID, args.Session)
	if err != nil {
		return tool.Prepared{}, err
	}
	description := capture.Description()
	var uploads *browser.NativeUploads
	capability, resource := "browser.external", description.Resource
	var uploadPaths []string
	if program != nil && len(program.UploadPaths()) > 0 {
		uploads, err = browser.CaptureNativeUploads(current.WorkingDirectory, program.UploadPaths())
		if err != nil {
			return tool.Prepared{}, err
		}
		resource, err = capture.UploadResource(uploads)
		if err != nil {
			return tool.Prepared{}, err
		}
		capability = "browser.external.upload"
		uploadPaths = uploads.Paths()
	}
	arguments, _ := json.Marshal(struct {
		Action       string   `json:"action"`
		Session      string   `json:"session"`
		Code         string   `json:"code"`
		Mode         string   `json:"mode"`
		Driver       string   `json:"driver"`
		Generation   string   `json:"generation"`
		WholeBrowser bool     `json:"whole_browser"`
		UploadPaths  []string `json:"upload_paths"`
	}{call.Name, description.Name, args.Code, description.Mode, description.Driver, description.Generation, true, uploadPaths})
	recheck := func(ctx context.Context) error {
		snapshot, err := r.configuration.Snapshot(ctx)
		if err != nil {
			return err
		}
		return capture.CheckConfig(snapshot.Host.ExternalBrowser, r.browserDriver(snapshot).Driver)
	}
	timeout := 60 * time.Second
	if args.Timeout != 0 {
		timeout = time.Duration(args.Timeout * float64(time.Second))
	}
	var lease *browser.NativeLease
	var images *hostImages
	return tool.Prepared{
		Capability: capability, Resource: resource, Arguments: arguments, Mutating: true, Lifetime: capture.Lifetime(), Timeout: timeout,
		Acquire: func(ctx context.Context) (func(), error) {
			if err := recheck(ctx); err != nil {
				return nil, err
			}
			releaseImages := func() {}
			if program != nil && program.Screenshots() > 0 {
				var err error
				images, releaseImages, err = r.acquireHostImages(ctx, current, call)
				if err != nil {
					return nil, err
				}
			}
			var err error
			lease, err = capture.Acquire(ctx)
			if err != nil {
				releaseImages()
				return nil, err
			}
			if err := recheck(ctx); err != nil {
				lease.Close()
				releaseImages()
				return nil, err
			}
			return func() { lease.Close(); releaseImages() }, nil
		}, Run: func(ctx context.Context, id session.OperationID) (any, error) {
			check := func(ctx context.Context) error {
				if err := recheck(ctx); err != nil {
					return err
				}
				return r.store.CheckDispatchedOperation(ctx, id)
			}
			result := externalBrowserResult{Session: description.Name, Mode: description.Mode, Generation: description.Generation, Availability: "connected", Screenshots: []session.ContentReference{}}
			if program == nil {
				err := lease.Detach(ctx, check)
				if err == nil {
					result.Availability = "detached"
				}
				return result, err
			}
			var snapshots map[string]string
			if uploads != nil {
				if err := check(ctx); err != nil {
					return result, err
				}
				var err error
				snapshots, err = uploads.Snapshot(ctx, lease, r.directory)
				if err != nil {
					_ = lease.Detach(ctx, check)
					return result, err
				}
			}
			err := lease.Run(ctx, check, func(ctx context.Context, backend browser.Backend) error {
				if uploads != nil {
					backend = browser.WithUploads(backend, snapshots)
				}
				limits := browser.ProgramLimits{}
				if images != nil {
					limits.Images, limits.ImageBytes = images.count, int(images.bytes)
				}
				var runErr error
				result.Output, runErr = program.Run(ctx, backend, limits, func(ctx context.Context, data []byte) error {
					publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					ref, err := r.publishHostImage(publishCtx, current.ID, id, len(result.Screenshots), "image/jpeg", data, images)
					if err == nil {
						result.Screenshots = append(result.Screenshots, ref)
					}
					return err
				})
				return runErr
			})
			if err != nil {
				result.Availability = "ended"
			}
			output := tool.Output{Value: result}
			if images != nil {
				output.ContentReferences = images.refs
			}
			return output, err
		},
	}, nil
}
