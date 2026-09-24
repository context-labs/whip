import type { MCPRefreshResult } from '@whip/protocol';

/** Discovery completion is not a promise that every server is connected. */
export function mcpRefreshNotice(result: MCPRefreshResult | undefined): string {
  const notices = ['MCP configuration refreshed for this session. Server connections may still be starting; check Integrations for their status.'];
  if (result?.changed?.length) notices.push(`${result.changed.length} existing server configuration(s) changed; a runtime reload is required to apply them. Refresh leaves their live configuration unchanged.`);
  if (result?.blocked?.length) notices.push(`${result.blocked.length} server(s) remain blocked.`);
  if (result?.source_errors?.length) notices.push(`${result.source_errors.length} configuration source(s) could not be read.`);
  return notices.join(' ');
}
