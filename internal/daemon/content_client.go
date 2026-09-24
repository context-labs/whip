package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

func (c *Client) ReadContent(ctx context.Context, params protocol.ContentReadParams) (protocol.ContentReadResult, error) {
	var result protocol.ContentReadResult
	err := c.Call(ctx, "content.read", params, &result)
	return result, err
}

func (c *Client) commandContent(ctx context.Context, rootID string, result *CommandResult) error {
	if result.Content == nil {
		return nil
	}
	handle := *result.Content
	if handle.Size < 0 || handle.Size > MaxUploadSize {
		return errors.New("command content exceeds transfer limit")
	}
	data := make([]byte, 0, int(handle.Size))
	for int64(len(data)) < handle.Size {
		chunk, err := c.ReadContent(ctx, protocol.ContentReadParams{RootID: rootID, ReferenceID: handle.ReferenceID, Offset: int64(len(data)), Limit: MaxContentChunk})
		if err != nil {
			return err
		}
		if chunk.Content != handle || len(chunk.Data) == 0 || int64(len(chunk.Data)) > handle.Size-int64(len(data)) {
			return errors.New("invalid command content response")
		}
		data = append(data, chunk.Data...)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != handle.Digest {
		return errors.New("command content digest mismatch")
	}
	result.Result = data
	fillCommandPresentation(result)
	return nil
}
