import { describe, expect, it } from 'vitest';
import type { Message } from '@whip/protocol';
import { designContextSummary } from '../src/browser-design-presentation';
import { messagePresentation, timelineRows } from '../src/conversation-rows';

const context = { context_attachment_id: 'context', screenshot_attachment_id: 'image', elements: [{ label: 'h1 · Heading', selector: '#heading' }], element_count: 1, page_url: 'https://example.com/settings', context_part_index: 1, screenshot_part_index: 2 };
const parts: Message['parts'] = [{ type: 'text', text: 'Make this smaller' }, { type: 'content', reference_id: 'context' }, { type: 'content', reference_id: 'image' }];
const message: Message = { id: 'message', session_id: 'child', group_id: 'turn', opening_input: true, turn_id: 'turn', input_id: 'input', mail: null, source: null, retired_by: null, retired_revision: null, sequence: '9007199254740993', role: 'user', parts, design_context: context, created_at: '2026-01-01T00:00:00Z' };

describe('Design Mode canonical message presentation', () => {
  it('preserves authored text and exact content references without hydrating evidence', () => {
    const result = messagePresentation([...parts, { type: 'text', text: 'Other text' }], context);
    expect(result.text).toBe('Make this smaller\n\nOther text');
    expect(result.references).toEqual(['context', 'image']);
    expect(result.designContext).toEqual(context);
  });
  it('never guesses provenance and retains references when coordinates or identities are malformed', () => {
    for (const invalid of [undefined, { ...context, context_part_index: 99 }, { ...context, context_attachment_id: 'other' }, { ...context, screenshot_part_index: 1 }]) {
      const result = messagePresentation(parts, invalid);
      expect(result.designContext).toBeUndefined();
      expect(result.references).toEqual(['context', 'image']);
    }
    expect(messagePresentation([...parts, { type: 'content', reference_id: 'context' }], context).designContext).toBeUndefined();
  });
  it('supports context without a screenshot and uses the same projection after history reload', () => {
    const withoutImage = { ...context, screenshot_attachment_id: undefined, screenshot_part_index: undefined };
    expect(messagePresentation(parts.slice(0, 2) as Message['parts'], withoutImage).designContext).toEqual(withoutImage);
    const rows = timelineRows({ snapshot: null, messages: [message], gaps: [], olderCursor: null, latestMissing: false });
    expect(rows[0]).toMatchObject({ text: 'Make this smaller', role: 'user', seq: '9007199254740993', references: ['context', 'image'], designContext: context });
  });
  it('creates bounded summaries from native capture without changing evidence', () => {
    const evidence = JSON.stringify({ schemaVersion: 1, title: 'Page', url: 'https://example.com', elements: Array.from({ length: 12 }, () => ({ tag: 'h1', name: '界'.repeat(100), selector: '#heading' })) });
    const summary = designContextSummary(evidence, 'context', 'image')!;
    expect(summary.element_count).toBe(12); expect(summary.elements).toHaveLength(8);
    expect(new TextEncoder().encode(summary.elements![0]!.label).length).toBeLessThanOrEqual(160);
    expect(summary.screenshot_attachment_id).toBe('image');
    expect(designContextSummary('plain text', 'context')).toBeUndefined();
  });
});
