package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) MCPTools(ctx context.Context, id session.SessionID, server string) ([]MCPTool, error) {
	owner, err := r.mcpMetadataOwner(ctx, id, server)
	if err != nil {
		return nil, err
	}
	value, err := r.mcpCatalog(ctx, owner, session.MCPCatalogRequest{Action: "list_tools", Server: server})
	if err != nil {
		return nil, err
	}
	return value.([]MCPTool), nil
}

func (r *Runtime) mcpMetadataOwner(ctx context.Context, id session.SessionID, server string) (session.Session, error) {
	if !mcpconfig.ValidName(server) {
		return session.Session{}, session.ErrInvalid
	}
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return owner, err
	}
	if !owner.Config.MCPServers.Contains(server) {
		return owner, store.ErrNotFound
	}
	return owner, nil
}

type MCPInstructions struct {
	Server, Generation, Resource, Text string
	ContentParts                       []session.ContentReference
	Bytes                              int64
}

// MCPInstructions observes the same delegated catalog as the model module. Large
// text uses owner-scoped content references, with no fabricated execution rows.
func (r *Runtime) MCPInstructions(ctx context.Context, id session.SessionID, server string) (MCPInstructions, error) {
	owner, err := r.mcpMetadataOwner(ctx, id, server)
	if err != nil {
		return MCPInstructions{}, err
	}
	value, err := r.mcpCatalog(ctx, owner, session.MCPCatalogRequest{Action: "instructions", Server: server})
	if err != nil {
		return MCPInstructions{}, err
	}
	fields := value.(map[string]any)
	result := MCPInstructions{Server: server, Generation: fields["generation"].(string), Resource: fields["resource"].(string), Text: fields["text"].(string), ContentParts: []session.ContentReference{}}
	result.Bytes = int64(len(result.Text))
	if len(result.Text) <= 64<<10 {
		return result, nil
	}
	identity := fmt.Sprintf("instructions_%x", sha256.Sum256([]byte(server+"\x00"+result.Generation+"\x00"+result.Text)))
	result.ContentParts, err = r.mcpContentParts(ctx, id, identity, "instructions", "text/plain", []byte(result.Text))
	result.Text = ""
	return result, err
}

func (r *Runtime) MCPBrandIcons(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) > 64 {
		return nil, store.ErrLimit
	}
	for _, key := range keys {
		_, canonical := mcp.BrandMetadata(mcp.ServerConfig{URL: "https://" + key})
		if canonical != key || key == "" {
			return nil, session.ErrInvalid
		}
	}
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot.Host.MCP.BrandIcons != nil && !*snapshot.Host.MCP.BrandIcons {
		return map[string]string{}, nil
	}
	ctx, done, err := r.beginMCPHuman(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return r.mcp.icons.Resolve(ctx, keys), nil
}
