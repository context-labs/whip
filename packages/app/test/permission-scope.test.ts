import { describe, expect, it } from 'vitest';
import { describeRule, splitGlobalRule } from '../src/permission-scope';

describe('permission scope wording', () => {
  it('reads an MCP selector as a sentence and keeps the digest as detail', () => {
    const rule = JSON.stringify({ server: 'docs', tool: 'search', definition: 'abc123' });
    expect(describeRule('mcp.call', rule)).toEqual({
      summary: 'MCP server docs, tool search, this exact definition only',
      detail: 'definition abc123',
    });
  });
  it('shows other rules as stored', () => {
    expect(describeRule('bash', 'git status')).toEqual({ summary: 'git status' });
    expect(describeRule('mcp.call', 'not json')).toEqual({ summary: 'not json' });
    expect(describeRule('mcp.call', '{"server":"docs"}')).toEqual({ summary: '{"server":"docs"}' });
  });
  it('splits a global host rule at its first colon only', () => {
    expect(splitGlobalRule('mcp.call:{"server":"docs","tool":"a:b","definition":"d"}')).toEqual({
      operation: 'mcp.call',
      rule: '{"server":"docs","tool":"a:b","definition":"d"}',
    });
    expect(splitGlobalRule('bash')).toEqual({ operation: 'bash', rule: '' });
  });
});
