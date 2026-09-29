package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/protocol"
)

// ResolveDefinition selects an advertised built-in or an exact registered revision.
// A bounded catalog scan never interprets multiple revisions as a mutable latest.
func (c *Client) ResolveDefinition(ctx context.Context, name string) (protocol.DefinitionRef, error) {
	if name == "" {
		name = "coding"
	}
	id, revision, explicit := strings.Cut(name, "@")
	if explicit {
		ref := protocol.DefinitionRef{ID: protocol.ID(id), Revision: revision}
		var document protocol.Definition
		if err := c.Call(ctx, "definitions.get", ref, &document); err != nil {
			return ref, err
		}
		return document.Ref, nil
	}
	for _, ref := range c.Builtins() {
		if string(ref.ID) == name {
			return ref, nil
		}
	}
	// Registered definitions are immutable. A name with multiple revisions has
	// no implicit mutable "latest" meaning; require the displayed exact revision.
	var match *protocol.DefinitionRef
	var after *protocol.DefinitionRef
	for range 100 {
		var page protocol.ListDefinitionsResult
		if err := c.Call(ctx, "definitions.list", protocol.ListDefinitionsParams{Limit: 100, After: after}, &page); err != nil {
			return protocol.DefinitionRef{}, err
		}
		for _, item := range page.Items {
			if string(item.Ref.ID) == name {
				if match != nil {
					return protocol.DefinitionRef{}, fmt.Errorf("agent %s has multiple revisions; use id@revision", name)
				}
				match = new(item.Ref)
			}
		}
		if page.NextCursor == nil {
			if match != nil {
				return *match, nil
			}
			return protocol.DefinitionRef{}, fmt.Errorf("unknown agent %q", name)
		}
		if after != nil && *after == *page.NextCursor {
			return protocol.DefinitionRef{}, errors.New("definition cursor did not advance")
		}
		after = page.NextCursor
	}
	return protocol.DefinitionRef{}, errors.New("definition catalog exceeds 10000 entries; select id@revision")
}
