export type RuleScope = { summary: string; detail?: string };

/**
 * Words for a saved permission rule. An MCP rule is a JSON selector binding
 * one exact server, tool and definition digest; a person should read the
 * scope as a sentence, with the digest available under a disclosure rather
 * than shown as opaque technical text. Other rules are shown as stored.
 */
export function describeRule(operation: string, rule: string): RuleScope {
  if (operation === 'mcp.call') {
    try {
      const selector = JSON.parse(rule) as { server?: unknown; tool?: unknown; definition?: unknown };
      if (typeof selector.server === 'string' && typeof selector.tool === 'string') {
        return {
          summary: `MCP server ${selector.server}, tool ${selector.tool}, this exact definition only`,
          detail: typeof selector.definition === 'string' ? `definition ${selector.definition}` : rule,
        };
      }
    } catch {
      // not a selector: show the rule as stored
    }
  }
  return { summary: rule };
}

/** Global host rules are stored as "operation:rule"; the rule may itself contain colons. */
export function splitGlobalRule(entry: string): { operation: string; rule: string } {
  const index = entry.indexOf(':');
  return index < 0 ? { operation: entry, rule: '' } : { operation: entry.slice(0, index), rule: entry.slice(index + 1) };
}
