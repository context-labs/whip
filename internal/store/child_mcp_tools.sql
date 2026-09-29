-- A child's automatic MCP authority is limited to tools captured at admission.
-- Missing historical catalogs cannot be reconstructed from a live connection.
CREATE TABLE child_mcp_tools (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 capability TEXT NOT NULL CHECK(capability='mcp.call.trusted'),
 resource TEXT NOT NULL CHECK(length(resource)=73 AND substr(resource,1,9)='mcp_call_'),
 PRIMARY KEY(session_id,capability,resource)
) STRICT;
CREATE TRIGGER child_mcp_tool_immutable BEFORE UPDATE ON child_mcp_tools
 BEGIN SELECT RAISE(ABORT, 'child MCP tool scope is immutable'); END;
CREATE TRIGGER child_mcp_tool_owner BEFORE INSERT ON child_mcp_tools
 WHEN NOT EXISTS(SELECT 1 FROM sessions WHERE id=NEW.session_id AND parent_id IS NOT NULL)
 BEGIN SELECT RAISE(ABORT, 'MCP delegation requires a child'); END;
