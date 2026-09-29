package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/gif" // Bounded decoding for supported host images.
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

// hostImages is one operation's bounded allowance. The existing live kernel's
// slot is held through SQL settlement, so another host call cannot consume the
// observed cell budget before this operation commits. Direct human work has no
// cell or kernel and receives only the per-operation allowance.
type hostImages struct {
	count int
	bytes int64
	refs  []string
}

func (r *Runtime) acquireHostImages(ctx context.Context, current session.Session, call tool.Invocation) (*hostImages, func(), error) {
	allowance := &hostImages{count: session.MaxOperationAttachments, bytes: session.MaxOperationAttachmentBytes}
	if call.CellID == "" {
		return allowance, func() {}, nil
	}
	entry, err := r.kernel(ctx, current.ID, current.HistoryRevision)
	if err != nil {
		return nil, nil, err
	}
	select {
	case entry.imageSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	release := func() { <-entry.imageSlot }
	allowance.count, allowance.bytes, err = r.store.CellAttachmentAllowance(ctx, call.CellID)
	if err != nil || allowance.count == 0 || allowance.bytes == 0 {
		release()
		if err == nil {
			err = store.ErrLimit
		}
		return nil, nil, err
	}
	return allowance, release, nil
}

func hostImageMedia(media string) bool {
	switch media {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

func (r *Runtime) publishHostImage(ctx context.Context, owner session.SessionID, operation session.OperationID, index int, media string, data []byte, allowance *hostImages) (session.ContentReference, error) {
	if allowance == nil || allowance.count < 1 || len(data) == 0 || len(data) > session.MaxContentBytes || int64(len(data)) > allowance.bytes || !hostImageMedia(media) {
		return session.ContentReference{}, store.ErrLimit
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != media || config.Width < 1 || config.Height < 1 || int64(config.Width) > 32_000_000/int64(config.Height) {
		return session.ContentReference{}, fmt.Errorf("%w: invalid or oversized host image", session.ErrInvalid)
	}
	if _, decodedFormat, err := image.Decode(bytes.NewReader(data)); err != nil || decodedFormat != format {
		return session.ContentReference{}, fmt.Errorf("%w: incomplete host image", session.ErrInvalid)
	}
	id := fmt.Sprintf("host_image_%x", sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", operation, index)))
	ref, err := r.PutContent(ctx, owner, id, media, data)
	if err != nil {
		return session.ContentReference{}, err
	}
	allowance.count--
	allowance.bytes -= int64(len(data))
	allowance.refs = append(allowance.refs, ref.ID)
	return ref, nil
}
