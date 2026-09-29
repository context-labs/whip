package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/browserhost"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// BrowserPeer belongs to one persistent transport. It only borrows the runtime
// owner, and must close/join on disconnect. It cannot reconnect or restore scopes.
type BrowserPeer struct {
	runtime *Runtime
	peer    *browserhost.Peer
}

func (r *Runtime) BrowserPeer() (*BrowserPeer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	peer, err := r.browser.OpenPeer()
	if err != nil {
		return nil, err
	}
	return &BrowserPeer{runtime: r, peer: peer}, nil
}

func (p *BrowserPeer) Bind(ctx context.Context, offer browserhost.Offer) (browserhost.Binding, error) {
	owner, err := p.runtime.store.Session(ctx, session.SessionID(offer.RootID))
	if err != nil {
		return browserhost.Binding{}, err
	}
	if owner.ParentID != nil || owner.Lifecycle != session.Active {
		return browserhost.Binding{}, store.ErrConflict
	}
	return p.peer.Bind(offer)
}
func (p *BrowserPeer) Unbind(root, epoch string) error { return p.peer.Unbind(root, epoch) }
func (p *BrowserPeer) Lifetime() context.Context       { return p.peer.Lifetime() }
func (p *BrowserPeer) Next(ctx context.Context) (browserhost.Notification, error) {
	return p.peer.Next(ctx)
}

func (p *BrowserPeer) Settle(result browserhost.CommandResult) error { return p.peer.Settle(result) }

func (p *BrowserPeer) SettleInventory(result browserhost.InventoryResult) error {
	return p.peer.SettleInventory(result)
}

func (p *BrowserPeer) UploadScreenshot(id, root, epoch, generation string, offset int, data []byte) error {
	return p.peer.UploadScreenshot(id, root, epoch, generation, offset, data)
}
func (p *BrowserPeer) Event(event browserhost.Event) error { return p.peer.Event(event) }
func (p *BrowserPeer) Close()                              { p.peer.Close() }
