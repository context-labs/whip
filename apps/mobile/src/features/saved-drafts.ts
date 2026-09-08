import type { SavedHost } from '../runtime/runtime';

type Answer = { selected: string[]; text: string; skipped: boolean };
type Question = [text: string, multiple: boolean, options: string[]];
export interface SavedDraftPresentation { title: string; detail: string; preview: string; copyText: string; note?: string }
const shortId = (id: string) => id.length > 14 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const onlyKeys = (value: Record<string, unknown>, keys: string[]) => Object.keys(value).every(key => keys.includes(key));
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(item => typeof item === 'string');
function answers(value: unknown): value is Answer[] {
  return Array.isArray(value) && value.length > 0 && value.length <= 16 && value.every(answer => object(answer)
    && onlyKeys(answer, ['selected', 'text', 'skipped']) && strings(answer.selected) && answer.selected.length <= 64
    && typeof answer.text === 'string' && typeof answer.skipped === 'boolean');
}

/** Export saved wording without matching it against a possibly changed live form. */
function questionText(text: string): Pick<SavedDraftPresentation, 'copyText' | 'preview' | 'note'> {
  const fallback = { copyText: text, preview: text, note: 'Unrecognized saved form. Copy preserves its original text.' };
  try {
    if (new TextEncoder().encode(text).byteLength > 64 << 10) return fallback;
    const value: unknown = JSON.parse(text);
    let saved: Answer[]; let questions: Question[] | undefined;
    if (answers(value)) saved = value; // Early mobile drafts stored answer pages alone.
    else {
      if (!object(value) || !onlyKeys(value, ['version', 'definition', 'answers']) || value.version !== 1 || typeof value.definition !== 'string' || !answers(value.answers)) return fallback;
      const definition: unknown = JSON.parse(value.definition);
      if (!Array.isArray(definition) || definition.length !== value.answers.length || !definition.every(entry => Array.isArray(entry)
        && entry.length === 3 && typeof entry[0] === 'string' && typeof entry[1] === 'boolean' && strings(entry[2]) && entry[2].length <= 64)) return fallback;
      questions = definition as Question[]; saved = value.answers;
    }
    const excerpts: string[] = [];
    const copyText = saved.map((answer, index) => {
      const question = questions?.[index];
      const heading = question ? `Question ${index + 1}\n${question[0]}` : `Saved answer ${index + 1}`;
      const response = [
        ...(answer.selected.length ? [`Selected answers:\n${answer.selected.map(label => `- ${label}`).join('\n')}`] : []),
        ...(answer.text.length ? [`Written answer:\n${answer.text}`] : []),
        ...(answer.skipped ? ['Marked as skipped.'] : !answer.selected.length && !answer.text.length ? ['No answer entered.'] : []),
      ].join('\n\n');
      excerpts.push(`${question?.[0] || `Saved answer ${index + 1}`}\n${[...answer.selected, answer.text].filter(value => value.length).join('\n') || (answer.skipped ? 'Skipped' : 'No answer entered')}`);
      const options = question?.[2].length ? `\n\nOptions (${question[1] ? 'choose any' : 'choose one'}):\n${question[2].map(label => `- ${label}`).join('\n')}` : '';
      return `${heading}${options}\n\n${response}`;
    }).join('\n\n———\n\n');
    return { copyText, preview: excerpts.join('\n\n'), ...(!questions ? { note: 'These saved answers do not include their original questions.' } : {}) };
  } catch { return fallback; }
}

export function savedDraftPresentation(key: string, text: string, hosts: readonly SavedHost[]): SavedDraftPresentation {
  const unknown = { title: 'Saved draft', detail: 'Unrecognized draft location', copyText: text, preview: text };
  let parts: unknown;
  try { parts = JSON.parse(key); } catch { return unknown; }
  if (!strings(parts) || parts.some(part => !part || part.length > 1024)) return unknown;
  const creation = parts.length === 3 && parts[0] === 'create';
  const question = parts.length === 4 && parts[2] === 'question';
  if (parts.length !== 3 && !question) return unknown;
  const runtimeId = parts[creation ? 1 : 0];
  const host = hosts.find(item => item.runtimeId === runtimeId);
  const server = (typeof host?.name === 'string' ? host.name.trim() : '') || `Server ${shortId(runtimeId)}${host ? '' : ' (not saved)'}`;
  if (creation) return { title: 'New-session prompt', detail: `${server} · Draft ${shortId(parts[2])}`, copyText: text, preview: text };
  const session = `${server} · Session ${shortId(parts[1])}`;
  if (question) return { title: 'Question answers', detail: `${session} · Request ${shortId(parts[3])}`, ...questionText(text) };
  return { title: parts[1] === parts[2] ? 'Message to root agent' : `Message to agent ${shortId(parts[2])}`, detail: session, copyText: text, preview: text };
}
