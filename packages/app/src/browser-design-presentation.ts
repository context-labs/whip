import type { SubmitPayload } from '@whip/protocol';
import { boundedDesignText } from './browser-design-geometry';

export type DesignContext = NonNullable<SubmitPayload['design_context']>;

/** Summarize our bounded native capture, never infer provenance from filenames. */
export function designContextSummary(text: string, contextId: string, screenshotId?: string): DesignContext | undefined {
  let evidence;
  try { evidence = JSON.parse(text); } catch { return undefined; }
  if (evidence?.schemaVersion !== 1 || !Array.isArray(evidence.elements) || !evidence.elements.length) return undefined;
  const clean = (value: unknown, max: number) => typeof value === 'string' ? boundedDesignText(value.replace(/\s+/gu, ' ').trim(), max) : '';
  return {
    context_attachment_id: contextId,
    ...(screenshotId ? { screenshot_attachment_id: screenshotId } : {}),
    element_count: Math.min(1000, evidence.elements.length),
    elements: evidence.elements.slice(0, 8).map((element: Record<string, unknown> | null) => {
      const tag = clean(element?.tag, 32), name = clean(element?.name || element?.text, 160);
      return { label: clean([tag, name].filter(Boolean).join(' · '), 160) || 'Selected element',
        ...(typeof element?.selector === 'string' ? { selector: clean(element.selector, 256) } : {}) };
    }),
    page_url: clean(evidence.url, 2048), page_title: clean(evidence.title, 256),
  };
}
