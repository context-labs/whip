import type { ThemeDefinition, DisplayPreferences } from '@whip/ui/theme-data';
import type { BrowserTarget } from './browser-types';

/** Serialized, bounded design-only boundary. No session, upload or browser-control grants. */
export interface BrowserDesignLease extends BrowserTarget { designId: string }
export interface BrowserDesignRevision extends BrowserDesignLease { documentRevision: number; selectionRevision: number }
export interface BrowserDesignBounds { x: number; y: number; width: number; height: number }
export type BrowserDesignColor = 'blue' | 'purple' | 'green' | 'orange';
export interface BrowserDesignElement {
  id: string;
  label: string;
  number: number;
  color: BrowserDesignColor;
  bounds: BrowserDesignBounds;
}
export interface BrowserDesignState extends BrowserDesignRevision {
  status: 'active' | 'capturing' | 'stale' | 'unavailable' | 'stopped';
  viewport: { width: number; height: number };
  elements: BrowserDesignElement[];
  /** Opaque hover identity stays stable for the same live backend node; independent of selection IDs. */
  hover?: BrowserDesignElement;
  /** Native increments on geometry invalidation, even when React batches the intervening hover clear. */
  hoverGeometryRevision?: number;
  error?: string;
}
export interface BrowserDesignRecipient { id: string; label: string; available: boolean }
export interface BrowserDesignDraft {
  prompt: string;
  /** Incremented only when admission clears the authored prompt, never for an edit echo. */
  promptReset?: number;
  recipients: BrowserDesignRecipient[];
  recipientId: string;
  screenshot: boolean;
  delivery: 'queue' | 'steer';
  busy: boolean;
  uncertain: boolean;
  error?: string;
  evidence?: { text: string; image?: string };
  theme: { name: string; mode: 'light' | 'dark'; palette?: ThemeDefinition; display?: DisplayPreferences };
}
export interface BrowserDesignModel { state: BrowserDesignState; draft: BrowserDesignDraft }
export type BrowserDesignIntent =
  | { kind: 'hover' | 'pick'; x: number; y: number; additive?: boolean }
  | { kind: 'scroll'; x: number; y: number; deltaX: number; deltaY: number }
  | { kind: 'remove'; id: string }
  | { kind: 'prompt'; value: string }
  | { kind: 'recipient'; id: string }
  | { kind: 'screenshot'; value: boolean }
  | { kind: 'delivery'; value: 'queue' | 'steer' }
  | { kind: 'pick-hover'; additive?: boolean }
  | { kind: 'capture' | 'send' | 'stop' | 'clear' | 'ancestor' | 'next' | 'previous' | 'evidence-close' };
export type BrowserDesignEvent =
  | { kind: 'state'; state: BrowserDesignState }
  | { kind: 'intent'; revision: BrowserDesignRevision; intent: BrowserDesignIntent };
export interface BrowserDesignCapture extends BrowserDesignRevision {
  capturedAt: string;
  /** Bounded UTF-8 JSON describing untrusted page evidence, never authored instructions. */
  text: string;
  /** PNG data URL, omitted on an explicit metadata-only capture. */
  image?: string;
}
export interface BrowserDesignPlatform {
  start(target: BrowserTarget): Promise<BrowserDesignState>;
  stop(lease: BrowserDesignLease): Promise<void>;
  update(input: BrowserDesignLease & { draft: BrowserDesignDraft }): Promise<void>;
  capture(input: BrowserDesignRevision & { screenshot: boolean }): Promise<BrowserDesignCapture>;
  onEvent(listener: (event: BrowserDesignEvent) => void): () => void;
}
/** Available only in the trusted, isolated overlay renderer. */
export interface BrowserDesignBridge {
  snapshot(): Promise<BrowserDesignModel | undefined>;
  onModel(listener: (model: BrowserDesignModel) => void): () => void;
  intent(input: { revision: BrowserDesignRevision; intent: BrowserDesignIntent }): Promise<void>;
}
export const browserDesignLimits = { selections: 16, prompt: 16_384, metadata: 65_536, recipients: 100 } as const;
