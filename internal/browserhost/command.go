package browserhost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

type pendingCommand struct {
	attachment *attachment
	command    Command
	ctx        context.Context
	result     chan Reply
	settled    bool
	image      []byte
}

func (h *Host) command(ctx context.Context, a *attachment, operationID, kind string, args json.RawMessage, expected string, check Check, scope *Scope) (Reply, error) {
	if check == nil {
		return Reply{}, errors.New("dispatched operation authority check required")
	}
	if e := ctx.Err(); e != nil {
		return Reply{}, e
	}
	if e := check(ctx); e != nil {
		return Reply{}, e
	}
	if !token(operationID) || !json.Valid(args) || len(args) > 256<<10 {
		return Reply{}, errors.New("invalid browser command")
	}
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		return Reply{}, errors.New("browser command deadline required")
	}
	h.mu.Lock()
	if !h.currentLocked(a.provider) || a.ctx.Err() != nil || (a.value.Delegated && kind != "detach") {
		h.mu.Unlock()
		return Reply{}, ErrStale
	}
	if expected != "" && expected != a.value.DocumentRevision {
		h.mu.Unlock()
		return Reply{}, &Failure{Kind: "stale_document", Message: "document changed before browser command"}
	}
	if len(h.pending) >= MaxPending {
		h.mu.Unlock()
		return Reply{}, ErrBusy
	}
	s := cloneScope(a.value.Scope)
	if scope != nil {
		s = cloneScope(*scope)
	}
	cmd := Command{CommandID: rand.Text(), OperationID: operationID, Identity: a.value.Owner, Scope: s, ExpectedDocument: expected, DeadlineMillis: deadline.UnixMilli(), Kind: kind, Arguments: slices.Clone(args)}
	pending := &pendingCommand{attachment: a, command: cmd, ctx: ctx, result: make(chan Reply, 1)}
	h.pending[cmd.CommandID] = pending
	delivered := a.provider.peer.notifyLocked(Notification{Command: &cmd})
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, cmd.CommandID)
		h.uploadBytes -= len(pending.image)
		pending.image = nil
		h.mu.Unlock()
	}()
	if !delivered {
		return Reply{}, ErrClosed
	}
	select {
	case reply := <-pending.result:
		if reply.Error != nil {
			if reply.Error.Kind == "outcome_unknown" {
				h.mu.Lock()
				h.revokeLocked(a)
				h.mu.Unlock()
				return reply, ErrUnknown
			}
			if reply.Error.Kind == "attachment_revoked" || reply.Error.Kind == "tab_closed" || reply.Error.Kind == "preview_disconnected" {
				h.mu.Lock()
				h.revokeLocked(a)
				h.mu.Unlock()
			}
			return reply, reply.Error
		}
		return reply, nil
	case <-ctx.Done():
	case <-a.ctx.Done():
	}
	h.mu.Lock()
	h.revokeLocked(a)
	a.provider.peer.dropCommandLocked(cmd.CommandID)
	a.provider.peer.notifyLocked(Notification{Cancel: &Cancel{CommandID: cmd.CommandID, RootID: cmd.Identity.RootID, ProviderEpoch: cmd.Scope.ProviderEpoch, AttachmentGeneration: cmd.Scope.AttachmentGeneration}})
	h.mu.Unlock()
	return Reply{}, ErrUnknown
}

func (p *Peer) dropCommandLocked(id string) {
	p.queue = slices.DeleteFunc(p.queue, func(q queued) bool {
		if q.value.Command != nil && q.value.Command.CommandID == id {
			p.bytes -= q.bytes
			return true
		}
		return false
	})
}

func (p *Peer) pendingLocked(id, root, epoch, generation string) (*pendingCommand, error) {
	q := p.host.pending[id]
	if p.closed || q == nil || q.settled || q.ctx.Err() != nil || q.attachment.ctx.Err() != nil || q.attachment.provider.peer != p || q.command.Identity.RootID != root || q.command.Scope.ProviderEpoch != epoch || q.command.Scope.AttachmentGeneration != generation || !p.host.currentLocked(q.attachment.provider) {
		return nil, ErrStale
	}
	return q, nil
}

func screenshotCommand(q *pendingCommand) bool {
	if q.command.Kind != "cdp" {
		return false
	}
	var args struct {
		Method string `json:"method"`
	}
	return json.Unmarshal(q.command.Arguments, &args) == nil && args.Method == "Page.captureScreenshot"
}

// UploadScreenshot accepts bounded sequential bytes for this connection's exact
// pending screenshot command. It is not a reusable global content capability.
func (p *Peer) UploadScreenshot(id, root, epoch, generation string, offset int, data []byte) error {
	if len(data) == 0 || len(data) > 64<<10 || offset < 0 {
		return errors.New("invalid screenshot chunk")
	}
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	q, e := p.pendingLocked(id, root, epoch, generation)
	if e != nil {
		return e
	}
	if !screenshotCommand(q) || offset != len(q.image) || len(data) > MaxScreenshotBytes-len(q.image) || len(data) > (16<<20)-h.uploadBytes {
		return errors.New("screenshot upload does not fit its pending command")
	}
	q.image = append(q.image, data...)
	h.uploadBytes += len(data)
	return nil
}

func (p *Peer) Settle(result CommandResult) error {
	if !textBound(result.DocumentRevision, 128) || !textBound(result.URL, 8192) || !textBound(result.Title, 512) || len(result.Result) > 512<<10 || !validFailure(result.Error) || (len(result.Result) > 0 && !json.Valid(result.Result)) {
		return errors.New("invalid or oversized browser result")
	}
	// Native callers cannot mutate queued results after acknowledgement.
	raw, e := json.Marshal(result)
	if e != nil {
		return errors.New("invalid browser result")
	}
	var copyResult CommandResult
	if json.Unmarshal(raw, &copyResult) != nil {
		return errors.New("invalid browser result")
	}
	result = copyResult
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	q, e := p.pendingLocked(result.CommandID, result.RootID, result.ProviderEpoch, result.AttachmentGeneration)
	if e != nil {
		return e
	}
	if result.Screenshot != nil {
		if result.Error != nil || !screenshotCommand(q) || result.Screenshot.MediaType != "image/jpeg" || result.Screenshot.Size <= 0 || result.Screenshot.Size != len(q.image) {
			return errors.New("screenshot does not match this pending command")
		}
		sum := sha256.Sum256(q.image)
		if result.Screenshot.Digest != hex.EncodeToString(sum[:]) {
			return errors.New("screenshot digest mismatch")
		}
		// Base64 image data must not be smuggled through ordinary CDP result text.
		if len(result.Result) > 0 && string(result.Result) != "{}" {
			return errors.New("screenshot result must use its uploaded bytes")
		}
	} else if result.Error == nil && (len(q.image) > 0 || screenshotCommand(q)) {
		return errors.New("screenshot acknowledgement requires complete uploaded bytes")
	}
	result.URL = cleanURL(result.URL)
	result.Title = cleanTitle(result.Title)
	q.settled = true
	image := slices.Clone(q.image)
	if result.Error != nil {
		image = nil
	}
	q.result <- Reply{CommandResult: result, Image: image}
	return nil
}

// Batch is valid only during its Lease.Run callback. Each call checks live
// dispatched authority again; a callback cannot keep it for another turn.
type Batch struct {
	lease       *Lease
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	operationID string
	check       Check
	events      []Event
	eventBytes  int
	eventReady  chan struct{}
	client      *CDPClient
	busy        bool // protected by host.mu; one CDP command at a time
	closed      bool
}

// NextEvent returns only this live batch's observations. Retired queues are not replayed.
func (b *Batch) NextEvent(ctx context.Context) (Event, error) {
	h := b.lease.capture.host
	for {
		h.mu.Lock()
		if b.closed || b.ctx.Err() != nil {
			h.mu.Unlock()
			return Event{}, ErrStale
		}
		if len(b.events) > 0 {
			event := b.events[0]
			b.events[0] = Event{}
			b.events = b.events[1:]
			raw, _ := json.Marshal(event)
			b.eventBytes -= len(raw)
			h.eventBytes -= len(raw)
			h.mu.Unlock()
			return event, nil
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-b.ctx.Done():
			return Event{}, ErrStale
		case <-b.eventReady:
		}
	}
}

func (l *Lease) Run(ctx context.Context, operationID string, check Check, run func(context.Context, *Batch) error) (Attachment, error) {
	if l.capture.operation != "run" || run == nil || !token(operationID) {
		return Attachment{}, errors.New("browser run callback required")
	}
	ctx, done, e := l.begin(ctx)
	if e != nil {
		return Attachment{}, e
	}
	defer done()
	if e = l.check(ctx, check); e != nil {
		return Attachment{}, e
	}
	h := l.capture.host
	a := l.attachment
	bctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b := &Batch{lease: l, ctx: bctx, cancel: cancel, operationID: operationID, check: check, eventReady: make(chan struct{}, 1)}
	h.mu.Lock()
	a.batch = b
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		b.closed = true
		cancel()
		if a.batch == b {
			a.batch = nil
		}
		client := b.client
		h.mu.Unlock()
		if client != nil {
			client.Close()
		}
		b.wg.Wait()
		h.mu.Lock()
		h.eventBytes -= b.eventBytes
		b.eventBytes = 0
		b.events = nil
		h.mu.Unlock()
	}()
	if _, e = h.command(bctx, a, operationID, "begin", json.RawMessage(`{}`), l.capture.args.ExpectedDocument, check, nil); e != nil {
		return Attachment{}, e
	}
	e = run(bctx, b)
	h.mu.Lock()
	b.closed = true
	client := b.client
	h.mu.Unlock()
	if client != nil {
		client.Close()
	}
	b.wg.Wait()
	// End is bounded teardown, never a retry. Authority revocation may make it
	// unavailable; peer disconnect must independently discard native batch state.
	if bctx.Err() == nil {
		endCtx, stop := context.WithTimeout(bctx, 2*time.Second)
		_, endErr := h.command(endCtx, a, operationID, "end", json.RawMessage(`{}`), "", check, nil)
		stop()
		if e == nil {
			e = endErr
		}
	}
	h.mu.Lock()
	out := cloneAttachment(a.value)
	if bctx.Err() != nil || errors.Is(e, ErrUnknown) {
		h.revokeLocked(a)
		if !errors.Is(e, ErrUnknown) {
			e = errors.Join(ErrUnknown, e)
		}
	}
	h.mu.Unlock()
	return out, e
}

// CDP enforces one synthetic target and session and rejects browser-wide/file
// access. The native provider must additionally enforce its own scoped policy.
func (b *Batch) CDP(ctx context.Context, sessionID, method string, params json.RawMessage) (Reply, error) {
	if !textBound(method, 256) || method == "" || len(params) > 256<<10 || !json.Valid(params) {
		return Reply{}, errors.New("invalid CDP request")
	}
	h := b.lease.capture.host
	a := b.lease.attachment
	if e := b.lease.check(ctx, b.check); e != nil {
		return Reply{}, e
	}
	h.mu.Lock()
	if b.closed || b.busy || b.ctx.Err() != nil || a.batch != b {
		h.mu.Unlock()
		return Reply{}, ErrStale
	}
	b.busy = true
	b.wg.Add(1)
	scope := cloneScope(a.value.Scope)
	title, address := a.value.Title, a.value.URL
	document := a.value.DocumentRevision
	h.mu.Unlock()
	defer func() { h.mu.Lock(); b.busy = false; h.mu.Unlock(); b.wg.Done() }()
	if sessionID != "" && sessionID != scope.AttachmentID {
		return Reply{}, errors.New("foreign CDP session")
	}
	deadline, _ := b.ctx.Deadline()
	ctx, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(b.ctx, cancel)
	defer func() { stop(); cancel() }()
	if strings.HasPrefix(method, "Target.") {
		var target struct {
			TargetID string `json:"targetId"`
		}
		if json.Unmarshal(params, &target) != nil {
			return Reply{}, errors.New("invalid target parameters")
		}
		if target.TargetID != "" && target.TargetID != scope.TabID {
			return Reply{}, errors.New("foreign CDP target")
		}
		info := map[string]any{"targetId": scope.TabID, "type": "page", "title": title, "url": address, "attached": true, "canAccessOpener": false}
		var value any
		switch method {
		case "Target.setDiscoverTargets":
			value = map[string]any{}
		case "Target.getTargets":
			value = map[string]any{"targetInfos": []any{info}}
		case "Target.getTargetInfo":
			value = map[string]any{"targetInfo": info}
		case "Target.attachToTarget":
			if target.TargetID != scope.TabID {
				return Reply{}, errors.New("target required")
			}
			value = map[string]string{"sessionId": scope.AttachmentID}
		default:
			return Reply{}, errors.New("unsupported target operation")
		}
		raw, _ := json.Marshal(value)
		return Reply{Result: raw}, nil
	}
	if strings.HasPrefix(method, "Browser.") || method == "DOM.setFileInputFiles" || method == "Page.printToPDF" {
		return Reply{}, errors.New("browser-wide and filesystem CDP operations unavailable")
	}
	raw, _ := json.Marshal(struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}{method, params})
	reply, e := h.command(ctx, a, b.operationID, "cdp", raw, document, b.check, nil)
	if e == nil {
		h.mu.Lock()
		if a.batch == b && a.ctx.Err() == nil {
			applyMetadata(&a.value, reply.CommandResult)
		}
		h.mu.Unlock()
	}
	return reply, e
}
