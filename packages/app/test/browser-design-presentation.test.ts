import { describe, expect, it } from 'vitest';
import { designContextSummary } from '../src/browser-design-presentation';
import { messagePresentation, timelineRows } from '../src/conversation-rows';

const context = { context_attachment_id: 'context', screenshot_attachment_id: 'image', elements: [{ label: 'h1 · Heading', selector: '#heading' }], element_count: 1, page_url: 'https://example.com/settings' };
const metadata = { version: 1, design_context: { ...context, context_part_index: 1, screenshot_part_index: 2 } };
const content = [
  { type: 'text', text: 'Make this smaller' },
  { type: 'text', text: 'Attachment "browser-design-context.txt": raw evidence' },
  { type: 'image_url', image_url: { url: 'data:image/png;base64,AA==' } },
];

describe('Design Mode message presentation', () => {
  it('groups exact evidence parts while preserving authored text and unrelated attachments', () => {
    const result = messagePresentation([...content, { type: 'text', text: 'Other attached text' }], metadata);
    expect(result.text).toBe('Make this smaller\n\nOther attached text');
    expect(result.images).toEqual([]);
    expect(result.designEvidence).toMatchObject({ rawText: content[1]!.text, context, image: { url: content[2]!.image_url!.url } });
  });
  it('does not guess from filenames and falls back without hiding malformed metadata', () => {
    expect(messagePresentation(content).text).toContain('browser-design-context.txt');
    expect(messagePresentation(content, { ...metadata, design_context: { ...metadata.design_context, context_part_index: 99 } }).text).toContain('browser-design-context.txt');
    expect(messagePresentation(content).images).toHaveLength(1);
  });
  it('supports context without a screenshot and reloads the same projection from history', () => {
    const presentation = { version: 1, design_context: { ...context, screenshot_attachment_id: undefined, context_part_index: 1 } };
    expect(messagePresentation(content.slice(0, 2), presentation).designEvidence?.image).toBeUndefined();
    const rows = timelineRows({ revision: 'r', messages: [{ seq: 1, message: { role: 'user', authored: true, content, presentation: metadata } }] } as any, undefined, true);
    expect(rows[0]?.text).toBe('Make this smaller');
    expect(rows[0]?.designEvidence?.context.element_count).toBe(1);
  });
  it('creates bounded summaries from native capture without changing evidence', () => {
    const evidence = JSON.stringify({ schemaVersion: 1, title: 'Page', url: 'https://example.com', elements: Array.from({ length: 12 }, () => ({ tag: 'h1', name: '界'.repeat(100), selector: '#heading' })) });
    const summary = designContextSummary(evidence, 'context', 'image')!;
    expect(summary.element_count).toBe(12); expect(summary.elements).toHaveLength(8);
    expect(new TextEncoder().encode(summary.elements[0]!.label).length).toBeLessThanOrEqual(160);
    expect(summary.screenshot_attachment_id).toBe('image');
    expect(designContextSummary('plain text', 'context')).toBeUndefined();
  });
});
