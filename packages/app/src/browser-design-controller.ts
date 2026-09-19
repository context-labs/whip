import type { BrowserTarget } from './browser-types';
import type { BrowserDesignCapture, BrowserDesignDraft, BrowserDesignEvent, BrowserDesignIntent, BrowserDesignPlatform, BrowserDesignRecipient, BrowserDesignState } from './browser-design-types';
import { browserDesignLimits } from './browser-design-types';
import { boundedDesignText, designLease, designRevision, sameDesignRevision } from './browser-design-geometry';

export interface DesignSubmission {
  recipientId: string; prompt: string; capture: BrowserDesignCapture; delivery: 'queue' | 'steer';
  current(): boolean;
  accepted(): void;
}
export interface DesignSubmissionResult { accepted: boolean; uncertain?: boolean; error?: string }
const message = (error: unknown) => boundedDesignText(error instanceof Error ? error.message : String(error), 2048);

/** Window-memory draft, separate from every destination's ordinary chat composition. */
export class BrowserDesignController {
  private state?: BrowserDesignState;
  private draft: BrowserDesignDraft;
  private capture?: BrowserDesignCapture;
  private evidenceOpen = false;
  private revision = 0;
  private recipientTouched = false;
  private operation = 0;
  private readonly listeners = new Set<() => void>();
  private off?: () => void;
  private snapshot: { state?: BrowserDesignState; draft: BrowserDesignDraft };
  constructor(private readonly platform: BrowserDesignPlatform, private readonly submit: (input: DesignSubmission) => Promise<DesignSubmissionResult>) {
    this.draft = { prompt: '', recipients: [], recipientId: '', screenshot: true, delivery: 'queue', busy: false, uncertain: false, theme: { name: 'auto', mode: 'light' } };
    this.snapshot = { draft: this.draft };
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish(sync = true, clearSelection?: Parameters<BrowserDesignPlatform['update']>[0]['clearSelection']) {
    this.snapshot = { state: this.state, draft: this.draft };
    for (const listener of this.listeners) listener();
    if (sync && this.state && this.state.status !== 'stopped') {
      const state = this.state, draft = this.draft;
      void this.platform.update({ ...designLease(state), draft, ...(clearSelection ? { clearSelection } : {}) }).catch(error => {
        if (this.state?.designId !== state.designId || this.draft !== draft) return;
        this.draft = { ...this.draft, error: message(error) }; this.publish(false);
      });
    }
  }
  configure(recipients: BrowserDesignRecipient[], associated: string[], theme: BrowserDesignDraft['theme']) {
    const choices = recipients.slice(0, browserDesignLimits.recipients).map(({ id, label, available }) => ({ id, label, available }));
    const previous = this.draft.recipients.find(item => item.id === this.draft.recipientId);
    if (previous && !choices.some(item => item.id === previous.id)) {
      if (choices.length === browserDesignLimits.recipients) choices.pop();
      choices.push({ ...previous, available: false });
    }
    const unique = [...new Set(associated)];
    const recipientId = this.draft.recipientId || (!this.recipientTouched && unique.length === 1 && choices.some(item => item.id === unique[0] && item.available) ? unique[0]! : '');
    this.draft = { ...this.draft, recipients: choices, recipientId, theme };
    this.publish();
  }
  async start(target: BrowserTarget) {
    if (this.state && this.state.status !== 'stopped' && this.state.status !== 'unavailable') return;
    const operation = ++this.operation;
    this.off?.();
    this.off = this.platform.onEvent(event => this.event(event));
    try {
      const state = await this.platform.start(target);
      if (operation !== this.operation) { await this.platform.stop(designLease(state)); return; }
      this.state = state; this.capture = undefined;
      this.draft = { ...this.draft, evidence: undefined, error: undefined };
      this.publish();
    } catch (error) {
      if (operation === this.operation) { this.off?.(); this.off = undefined; this.draft = { ...this.draft, error: message(error) }; this.publish(false); }
      throw error;
    }
  }
  async stop() {
    ++this.operation;
    const state = this.state;
    this.off?.(); this.off = undefined;
    this.state = state ? { ...state, status: 'stopped', elements: [], hover: undefined } : undefined;
    this.capture = undefined; this.evidenceOpen = false; this.draft = { ...this.draft, evidence: undefined, busy: false };
    this.publish(false);
    if (state) await this.platform.stop(designLease(state));
  }
  private event(event: BrowserDesignEvent) {
    const state = this.state;
    if (!state) return;
    const incoming = event.kind === 'state' ? event.state : event.revision;
    if (incoming.designId !== state.designId || incoming.epoch !== state.epoch || incoming.tabId !== state.tabId || incoming.generation !== state.generation) return;
    if (event.kind === 'intent') { if (sameDesignRevision(state, event.revision)) void this.intent(event.intent); return; }
    if (incoming.documentRevision < state.documentRevision || (incoming.documentRevision === state.documentRevision && incoming.selectionRevision < state.selectionRevision)) return;
    const invalidated = !sameDesignRevision(state, event.state);
    if (invalidated) { this.capture = undefined; this.draft = { ...this.draft, evidence: undefined }; }
    this.state = event.state;
    if (event.state.status === 'stopped' || event.state.status === 'unavailable') { ++this.operation; this.draft = { ...this.draft, busy: false }; }
    this.publish(invalidated);
  }
  async intent(intent: BrowserDesignIntent) {
    if (intent.kind === 'stop') { await this.stop(); return; }
    if (intent.kind === 'evidence-close') { this.evidenceOpen = false; this.draft = { ...this.draft, evidence: undefined }; this.publish(); return; }
    if (this.draft.busy || this.draft.uncertain) return;
    switch (intent.kind) {
      case 'prompt': this.draft = { ...this.draft, prompt: boundedDesignText(intent.value, browserDesignLimits.prompt), error: undefined }; ++this.revision; break;
      case 'recipient':
        this.recipientTouched = true;
        if (intent.id && !this.draft.recipients.some(item => item.id === intent.id && item.available)) return;
        this.draft = { ...this.draft, recipientId: intent.id, error: undefined }; ++this.revision; break;
      case 'screenshot': this.capture = undefined; this.draft = { ...this.draft, screenshot: intent.value, evidence: undefined, error: undefined }; ++this.revision; break;
      case 'delivery': this.draft = { ...this.draft, delivery: intent.value }; ++this.revision; break;
      case 'capture': this.evidenceOpen = true; await this.captureEvidence(); return;
      case 'send': await this.send(); return;
      default: return;
    }
    this.publish();
  }
  private async captureEvidence() {
    const state = this.state, screenshot = this.draft.screenshot;
    if (!state || !state.elements.length || state.status !== 'active') return;
    const operation = ++this.operation;
    this.draft = { ...this.draft, busy: true, error: undefined }; this.publish();
    try {
      const capture = await this.platform.capture({ ...designRevision(state), screenshot });
      if (operation !== this.operation || !this.state || !sameDesignRevision(this.state, capture)) return;
      if (new TextEncoder().encode(capture.text).length > browserDesignLimits.metadata || (capture.image && (!capture.image.startsWith('data:image/png;base64,') || capture.image.length > 12 * 1024 * 1024))) throw new Error('The evidence exceeds capture limits.');
      this.capture = capture;
      this.draft = { ...this.draft, evidence: this.evidenceOpen ? { text: capture.text, image: capture.image } : undefined };
    } catch (error) { if (operation === this.operation) this.draft = { ...this.draft, error: message(error) }; }
    finally { if (operation === this.operation) { this.draft = { ...this.draft, busy: false }; this.publish(); } }
  }
  private async send() {
    if (!this.draft.prompt.trim() || !this.draft.recipients.some(item => item.id === this.draft.recipientId && item.available)) {
      this.draft = { ...this.draft, error: 'Describe the change and choose an available conversation.' }; this.publish(); return;
    }
    // Always capture after the intentional Send action; previews are not live evidence.
    await this.captureEvidence();
    const capture = this.capture, state = this.state;
    if (!capture || !state || state.status !== 'active' || !sameDesignRevision(state, capture) || this.draft.error) return;
    const frozen = this.draft, revision = this.revision, operation = ++this.operation;
    this.draft = { ...frozen, busy: true, error: undefined }; this.publish();
    const current = () => operation === this.operation && this.revision === revision && !!this.state && this.state.status === 'active' && sameDesignRevision(this.state, capture) && this.draft.recipients.some(item => item.id === frozen.recipientId && item.available);
    const accepted = () => {
      // Admission may resolve after navigation/closure. Only clear the exact authored draft.
      if (revision !== this.revision) return;
      ++this.revision;
      this.capture = undefined;
      this.evidenceOpen = false;
      // Native owns selection: clear only the evidence that was actually admitted.
      const clearSelection = this.state && sameDesignRevision(this.state, capture)
        ? { documentRevision: capture.documentRevision, selectionRevision: capture.selectionRevision } : undefined;
      this.draft = { ...this.draft, prompt: '', promptReset: (this.draft.promptReset ?? 0) + 1, busy: false, uncertain: false, evidence: undefined, error: undefined }; this.publish(true, clearSelection);
    };
    try {
      const result = await this.submit({ recipientId: frozen.recipientId, prompt: frozen.prompt, capture, delivery: frozen.delivery, current, accepted });
      if (operation !== this.operation || result.accepted) return;
      this.draft = { ...this.draft, busy: false, uncertain: !!result.uncertain, error: result.error ? message(result.error) : undefined }; this.publish();
    } catch (error) { if (operation === this.operation) { this.draft = { ...this.draft, busy: false, error: message(error) }; this.publish(); } }
  }
}
