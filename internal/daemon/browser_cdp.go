package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-rod/rod/lib/cdp"
)

// browserCDP has no socket and no reconnect/replay path. Rod sees precisely one
// synthetic target/session; native remains authoritative for exact CDP policy.
type browserCDP struct {
	providers   *browserProviders
	attachment  *browserAttachment
	operationID string
	events      chan *cdp.Event
}

func (c *browserCDP) Event() <-chan *cdp.Event { return c.events }
func (c *browserCDP) Call(ctx context.Context, sessionID, method string, params any) ([]byte, error) {
	p, a := c.providers, c.attachment
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	scope := cloneBrowserScope(a.scope)
	grant := a.grant
	identity := a.identity
	document := a.result.DocumentRevision
	url, title := a.result.URL, a.result.Title
	dead := a.ctx.Err() != nil || a.delegated
	p.mu.Unlock()
	if dead {
		return nil, browserFailure("attachment_revoked", "browser CDP attachment revoked")
	}
	if sessionID != "" && sessionID != scope.AttachmentID {
		return nil, browserFailure("permission_denied", "foreign CDP session")
	}
	if err := p.store.AuthorizeBrowser(ctx, identity.RootID, identity.AgentID, grant, "browser.run", scope); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var target struct {
		TargetID string `json:"targetId"`
	}
	if strings.HasPrefix(method, "Target.") {
		if err = json.Unmarshal(encoded, &target); err != nil {
			return nil, err
		}
		if target.TargetID != "" && target.TargetID != scope.TabID {
			return nil, browserFailure("permission_denied", "foreign CDP target")
		}
		info := map[string]any{"targetId": scope.TabID, "type": "page", "title": title, "url": url, "attached": true, "canAccessOpener": false}
		switch method {
		case "Target.setDiscoverTargets":
			return []byte(`{}`), nil
		case "Target.getTargets":
			return json.Marshal(map[string]any{"targetInfos": []any{info}})
		case "Target.getTargetInfo":
			return json.Marshal(map[string]any{"targetInfo": info})
		case "Target.attachToTarget":
			if target.TargetID != scope.TabID {
				return nil, browserFailure("permission_denied", "target is required")
			}
			return json.Marshal(map[string]string{"sessionId": scope.AttachmentID})
		default:
			return nil, browserFailure("unsupported_operation", "target operation is not available")
		}
	}
	if strings.HasPrefix(method, "Browser.") || method == "DOM.setFileInputFiles" || method == "Page.printToPDF" {
		return nil, browserFailure("unsupported_operation", "browser-wide and filesystem CDP operations are not available")
	}
	raw, err := json.Marshal(struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}{Method: method, Params: encoded})
	if err != nil {
		return nil, err
	}
	result, err := p.command(ctx, a, c.operationID, "cdp", raw, document)
	if err != nil {
		return nil, err
	}
	if method == "Page.captureScreenshot" {
		if result.Screenshot == nil {
			return nil, errors.New("screenshot requires a same-connection uploaded content reference")
		}
		handle := result.Screenshot
		data := make([]byte, 0, handle.Size)
		for int64(len(data)) < handle.Size {
			chunk, meta, readErr := p.store.ReadContent(ctx, handle.ReferenceID, identity.RootID, identity.AgentID, int64(len(data)), MaxContentChunk)
			if readErr != nil {
				return nil, readErr
			}
			if len(chunk) == 0 || meta.Digest != handle.Digest || meta.Size != handle.Size || meta.MediaType != "image/jpeg" || int64(len(data)+len(chunk)) > handle.Size {
				return nil, errors.New("screenshot content metadata mismatch")
			}
			data = append(data, chunk...)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != handle.Digest {
			return nil, errors.New("screenshot digest mismatch")
		}
		return json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(data)})
	}
	if len(result.Result) == 0 {
		return []byte(`{}`), nil
	}
	return result.Result, nil
}
