package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/computerconfig"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

type ComputerStatus struct {
	Revision         string
	Config           computerconfig.Config
	Control          computer.ControllerStatus
	BundledAvailable bool
}

// ComputerStatus reads availability without probing, extracting or starting a
// helper. Adopting a changed policy only invalidates old control authority.
func (r *Runtime) ComputerStatus(ctx context.Context) (ComputerStatus, error) {
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	return r.computerStatus(ctx)
}

func (r *Runtime) computerStatus(ctx context.Context) (ComputerStatus, error) {
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return ComputerStatus{}, err
	}
	if err := r.computer.Update(snapshot.Host.Computer); err != nil {
		return ComputerStatus{}, err
	}
	settings, err := snapshot.Host.Computer.Normalize()
	return ComputerStatus{Revision: snapshot.Revision, Config: settings, Control: r.computer.Status(), BundledAvailable: computer.BundledAvailable()}, err
}

// UseBundledComputer explicitly publishes an embedded helper. It preserves
// policy and refuses to replace an already configured executable. Configuration
// remains the authority; an extracted file alone never enables control.
func (r *Runtime) UseBundledComputer(ctx context.Context, expected string) (ComputerStatus, error) {
	return r.useBundledComputer(ctx, expected, computer.PublishBundled)
}

func (r *Runtime) useBundledComputer(ctx context.Context, expected string, publish func(context.Context, string) (string, error)) (ComputerStatus, error) {
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return ComputerStatus{}, err
	}
	if expected == "" || snapshot.Revision != expected || snapshot.Host.Computer.HelperExecutable != "" {
		return ComputerStatus{}, config.ErrRevisionConflict
	}
	path, err := publish(ctx, r.directory)
	if err != nil {
		return ComputerStatus{}, err
	}
	_, err = r.configuration.Update(ctx, expected, func(host *config.Host) error {
		host.Computer.HelperExecutable = path
		return nil
	})
	// A concurrent host update can leave a verified, unreferenced file, but
	// cannot cause publication to overwrite policy or a newer explicit path.
	status, refreshErr := r.computerStatus(context.WithoutCancel(ctx))
	return status, errors.Join(err, refreshErr)
}

func (r *Runtime) ConfigureComputer(ctx context.Context, expected string, settings computerconfig.Config) (ComputerStatus, error) {
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	settings, err := settings.Normalize()
	if err != nil {
		return ComputerStatus{}, errors.Join(session.ErrInvalid, err)
	}
	_, err = r.configuration.Update(ctx, expected, func(host *config.Host) error { host.Computer = settings; return nil })
	// A directory-sync error may follow publication. Even then, retire control
	// under an obsolete policy; never report that the write is durable.
	status, refreshErr := r.computerStatus(context.WithoutCancel(ctx))
	return status, errors.Join(err, refreshErr)
}

// ChangeComputerConnection is explicit generation-CAS human control. A lost
// reconnect acknowledgment requires a fresh status, never another hidden reset.
func (r *Runtime) ChangeComputerConnection(ctx context.Context, generation string, reconnect bool) (ComputerStatus, error) {
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	status, err := r.computerStatus(ctx)
	if err != nil {
		return ComputerStatus{}, err
	}
	if status.Control.Generation != generation {
		return ComputerStatus{}, store.ErrConflict
	}
	if reconnect {
		err = r.computer.Reconnect()
	} else {
		r.computer.Disconnect()
	}
	status.Control = r.computer.Status()
	return status, err
}

func (r *Runtime) prepareComputer(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var args struct {
		Code string `json:"code"`
	}
	if call.Name != "run" {
		return tool.Prepared{}, session.ErrInvalid
	}
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, session.ErrInvalid
	}
	batch, err := computer.CompileBatch(args.Code)
	if err != nil {
		return tool.Prepared{}, errors.Join(session.ErrInvalid, err)
	}
	if _, err := r.ComputerStatus(ctx); err != nil {
		return tool.Prepared{}, err
	}
	capture, err := r.computer.Capture(batch)
	if err != nil {
		return tool.Prepared{}, err
	}
	capability := "computer.run.trusted"
	if capture.NeedsConsent() {
		capability = "computer.run"
	}
	if batch.Intent().BroadScript {
		capability = "computer.applescript"
	}
	arguments, err := json.Marshal(struct {
		Code   string               `json:"code"`
		Intent computer.BatchIntent `json:"intent"`
	}{args.Code, batch.Intent()})
	if err != nil {
		return tool.Prepared{}, err
	}
	recheck := func(ctx context.Context) error {
		snapshot, err := r.configuration.Snapshot(ctx)
		if err != nil {
			return err
		}
		return capture.CheckConfig(snapshot.Host.Computer)
	}
	var lease *computer.Lease
	var images *hostImages
	observations := &computer.Observations{}
	return tool.Prepared{
		Capability: capability, Resource: capture.Resource(), Arguments: arguments, Mutating: true, Lifetime: capture.Lifetime(), Timeout: 5 * time.Minute,
		Acquire: func(ctx context.Context) (func(), error) {
			if err := recheck(ctx); err != nil {
				return nil, err
			}
			releaseImages := func() {}
			if batch.MayCaptureImages() {
				images, releaseImages, err = r.acquireHostImages(ctx, current, call)
				if err != nil {
					return nil, err
				}
			}
			if call.CellID != "" {
				entry, failure := r.kernel(ctx, current.ID, current.HistoryRevision)
				if failure != nil {
					releaseImages()
					return nil, failure
				}
				observations = entry.computerObservations
			}
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
			count, size := session.MaxOperationAttachments, session.MaxOperationAttachmentBytes
			if images != nil {
				count, size = images.count, images.bytes
			}
			result, runErr := lease.RunBounded(ctx, observations, func(ctx context.Context) error {
				if err := recheck(ctx); err != nil {
					return err
				}
				return r.store.CheckDispatchedOperation(ctx, id)
			}, count, size)
			publishCtx, cancelPublish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancelPublish()
			refs := []session.ContentReference{}
			for index, data := range result.Screenshots {
				ref, writeErr := r.publishHostImage(publishCtx, current.ID, id, index, "image/jpeg", data, images)
				if writeErr != nil {
					runErr = errors.Join(runErr, writeErr)
					break
				}
				refs = append(refs, ref)
			}
			output := tool.Output{Value: map[string]any{"text": result.Text, "screenshots": refs}}
			if images != nil {
				output.ContentReferences = images.refs
			}
			return output, runErr
		},
	}, nil
}
