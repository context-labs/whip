package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// PublishModelCapture publishes bounded bytes before the attempt transaction
// installs their access references. Failure records absence, not a stale body.
func (r *Runtime) PublishModelCapture(ctx context.Context, owner session.SessionID, capture session.ModelCapture) (session.ModelCapture, error) {
	chunks := map[string][]byte{}
	for _, body := range []*session.CapturedText{&capture.Instructions, &capture.Notices} {
		body.Chunks = []session.ContentReference{}
		if body.Status != "available" {
			continue
		}
		for data := body.Data; len(data) > 0; {
			size := min(len(data), session.MaxContentBytes)
			piece := data[:size]
			data = data[size:]
			digest := session.CaptureDigest(piece)
			ref := session.ContentReference{ID: "model_" + digest, SessionID: owner, Digest: digest, Size: int64(size), MediaType: "text/plain"}
			body.Chunks = append(body.Chunks, ref)
			chunks[ref.ID] = piece
		}
		body.Data = nil
	}
	if err := capture.Validate(owner); err != nil {
		return capture, err
	}
	capacity, err := r.store.ModelCaptureCapacity(ctx, owner, capture)
	if err != nil {
		return capture, err
	}
	for _, body := range []*session.CapturedText{&capture.Instructions, &capture.Notices} {
		if body.Status != "available" {
			continue
		}
		if !capacity && body.Bytes > 0 {
			body.Status, body.Chunks = "quota", []session.ContentReference{}
			continue
		}
		for _, chunk := range body.Chunks {
			if err := ctx.Err(); err != nil {
				return capture, err
			}
			if _, err := r.content.Put(chunks[chunk.ID]); err != nil {
				body.Status, body.Chunks = "storage_error", []session.ContentReference{}
				break
			}
		}
	}
	return capture, nil
}

func (r *Runtime) ModelInspection(ctx context.Context, owner session.SessionID, id session.ModelAttemptID) (session.ModelInspection, error) {
	return r.store.ModelInspection(ctx, owner, id)
}

// readCapturedText verifies the complete bounded text in addition to each chunk.
func (r *Runtime) readCapturedText(ctx context.Context, owner session.SessionID, body session.CapturedText, limit int64) (string, error) {
	if body.Status != "available" || body.Bytes > limit {
		return "", fmt.Errorf("%w: captured text unavailable within export limit", store.ErrLimit)
	}
	var result strings.Builder
	result.Grow(int(body.Bytes))
	for _, chunk := range body.Chunks {
		_, data, err := r.ReadContent(ctx, owner, chunk.ID, session.MaxContentBytes)
		if err != nil {
			return "", err
		}
		result.Write(data)
	}
	value := result.String()
	if int64(len(value)) != body.Bytes || session.CaptureDigest([]byte(value)) != body.Digest {
		return "", fmt.Errorf("%w: captured text digest mismatch", session.ErrInvalid)
	}
	return value, nil
}
