/** A deliberately conservative subset: never render a successful partial parse. */
export type MermaidType = 'flowchart' | 'state' | 'sequence' | 'class' | 'er' | 'xychart';
export type MermaidReason = 'empty' | 'too-large' | 'unsupported-type' | 'unsupported-syntax' | 'invalid-syntax' | 'unsafe-source' | 'busy' | 'unavailable' | 'timeout' | 'render-failed' | 'output-limit';
export interface MermaidPalette {
  background: string;
  foreground: string;
  line: string;
  accent: string;
  muted: string;
  surface: string;
  border: string;
  font: 'inter' | 'system';
  fontSize: number;
}
export interface MermaidImage {svg: string; width: number; height: number}
export type MermaidValidation = {ok: true; type: MermaidType} | {ok: false; reason: MermaidReason; message: string};
export const mermaidLimits = {sourceBytes: 32 * 1024, statements: 250, lineLength: 2048, outputBytes: 1024 * 1024, dimension: 16384, pixels: 16 * 1024 * 1024, queue: 16, queueBytes: 512 * 1024, cache: 32, cacheBytes: 8 * 1024 * 1024, timeoutMs: 2000, startupMs: 15000} as const;
export const mermaidVersion = 'beautiful-mermaid@1.1.3/whip-1';
export const mermaidBytes = (text: string) => new TextEncoder().encode(text).byteLength;
export class MermaidRenderError extends Error {
  readonly reason: MermaidReason;
  constructor(reason: MermaidReason, message: string) {super(message); this.name = 'MermaidRenderError'; this.reason = reason;}
}
const fail = (reason: MermaidReason, message: string): MermaidValidation => ({ok: false, reason, message});
const id = '[A-Za-z_][A-Za-z_0-9]*';
const label = '[^<>;`\\[\\]{}()|]+';
const node = `${id}(?:\\[${label}\\]|\\(${label}\\)|\\{${label}\\}|\\(\\(${label}\\)\\))?`;
const flow = new RegExp(`^${node}(?:\\s+(?:<-->|<-\\.->|<==>|-->|---|-\\.->|-\\.-|==>|===)(?:\\|${label}\\|)?\\s+${node})*$`);
const stateTransition = new RegExp(`^(?:${id}|\\[\\*\\])\\s+-->\\s+(?:${id}|\\[\\*\\])(?:\\s*:\\s*.+)?$`);
const stateLabel = new RegExp(`^(?:state\\s+"[^"{}]+"\\s+as\\s+${id}|${id}\\s*:\\s*[^{}]+)$`);
const participant = new RegExp(`^(participant|actor)\\s+(${id})(?:\\s+as\\s+(.+))?$`);
const message = new RegExp(`^(${id})\\s*(--?>>|--?\\))\\s*(${id})\\s*:\\s*(.+)$`);
const note = new RegExp(`^Note\\s+(?:left of|right of|over)\\s+${id}(?:,\\s*${id})?\\s*:\\s*.+$`, 'i');
const classDeclaration = new RegExp(`^class\\s+${id}(?:\\s*\\{)?$`);
const member = /^[+\-#~]?[A-Za-z_][\w]*(?:\[\]|~\w+~)?(?:\s+[A-Za-z_]\w*)?(?:\([^(){}]*\)(?:\s+[\w[\]]+)?)?$/;
const classRelation = new RegExp(`^${id}\\s+(?:"[^"{}]+"\\s+)?(?:<\\|--|<\\|\\.\\.|\\*--|o--|-->|--\\*|--o|--\\|>|\\.\\.>|\\.\\.\\|>|<--)\\s+(?:"[^"{}]+"\\s+)?${id}(?:\\s*:\\s*[^{}]+)?$`);
const erRelation = new RegExp(`^${id}\\s+(?:\\|\\||o\\||\\|o|}\\||\\|\\{)(?:--|\\.\\.)(?:\\|\\||o\\||\\|o|\\|\\{|o\\{)\\s+${id}\\s*:\\s*[^{}]+$`);
const erAttribute = /^[A-Za-z_][\w[\]]*\s+[A-Za-z_]\w*(?:\s+(?:PK|FK|UK))*$/;
const number = '-?\\d+(?:\\.\\d+)?';
const numericList = new RegExp(`^${number}(?:\\s*,\\s*${number})*$`);
const axisRange = new RegExp(`^(x-axis|y-axis)\\s+(?:"[^"<>]+"\\s+)?(${number})\\s+-->\\s+(${number})$`);

export function validateMermaidPalette(palette: MermaidPalette): void {
  for (const key of ['background', 'foreground', 'line', 'accent', 'muted', 'surface', 'border'] as const) {
    if (!/^#[0-9a-f]{6}$/i.test(palette[key])) throw new MermaidRenderError('render-failed', 'The diagram needs resolved theme colors.');
  }
  if (!['inter', 'system'].includes(palette.font) || !Number.isFinite(palette.fontSize) || palette.fontSize < 12 || palette.fontSize > 20) throw new MermaidRenderError('render-failed', 'The diagram display settings are invalid.');
}

export function validateMermaid(source: string): MermaidValidation {
  if (source.length > mermaidLimits.sourceBytes || mermaidBytes(source) > mermaidLimits.sourceBytes) return fail('too-large', 'Diagram source exceeds 32 KiB.');
  if (!source.trim()) return fail('empty', 'The diagram has no source yet.');
  // Configuration, markup and entity decoding can change parser meaning. Source is not a CSS/URL authority.
  if (/^\s*(?:---|%%\s*\{)|<\/?[A-Za-z!]|&(?:#\w+|[A-Za-z]+);|\b(?:javascript|data|https?):|\u0000/im.test(source)) return fail('unsafe-source', 'Diagram configuration, HTML and external resources are not supported.');
  const lines = source.split(/\r?\n/).map(line => line.trim()).filter(line => line && !line.startsWith('%%'));
  const header = lines.shift() ?? '';
  let type: MermaidType;
  if (/^(?:flowchart|graph) (?:TD|TB|BT|LR|RL)$/.test(header)) type = 'flowchart';
  else if (/^stateDiagram(?:-v2)?$/.test(header)) type = 'state';
  else if (header === 'sequenceDiagram') type = 'sequence';
  else if (header === 'classDiagram') type = 'class';
  else if (header === 'erDiagram') type = 'er';
  else if (/^xychart(?:-beta)?(?: horizontal)?$/.test(header)) type = 'xychart';
  else return fail('unsupported-type', 'This diagram type or header is not supported.');
  if (!lines.length) return fail('invalid-syntax', 'The diagram is incomplete.');
  if (lines.length > mermaidLimits.statements || lines.some(line => line.length > mermaidLimits.lineLength)) return fail('too-large', 'This diagram has too many statements or an oversized label.');
  if (lines.some(line => /;|`|:::|%%|^\s*(?:click|style|classDef|linkStyle|accTitle|accDescr)\b/.test(line))) return fail('unsupported-syntax', 'Diagram styling, actions, accessibility directives and multi-statement lines are not supported.');
  // ponytail: validate a conservative, fully consumed grammar rather than fork the permissive upstream parsers.
  // New syntax belongs here only with a fixture proving every statement survives rendering faithfully.
  let body = false;
  let group = false;
  let graphContent = false;
  const groupIds = new Set<string>();
  const flowNodes = new Set<string>();
  const nodeGroups = new Map<string, string | undefined>();
  let groupId: string | undefined;
  const blocks: string[] = [];
  const declaredActors = new Set<string>();
  const usedActors = new Set<string>();
  const stateIds = new Set<string>();
  let stateContent = false;
  let stateDirection = false;
  const chartKeys = new Set<string>();
  let chartPoints: number | undefined;
  let seriesCount = 0;
  let messages = 0;
  for (const line of lines) {
    let accepted = false;
    if (type === 'flowchart') {
      const start = line.match(/^subgraph ([A-Za-z_][A-Za-z_0-9]*)(?:\s*\[([^\[\]<>]+)\])?$/);
      if (start) {accepted = !group && !groupIds.has(start[1]!); group = true; groupIds.add(start[1]!); groupId = start[1]!;}
      else if (line === 'end') {accepted = group; group = false; groupId = undefined;}
      else {
        accepted = flow.test(line) && !/^(?:subgraph|direction)\b/.test(line); graphContent = true;
        if (accepted) {
          // Upstream keeps the first node definition, including an implicit edge endpoint.
          const tokens = new RegExp(`${node}|(\\s+(?:<-->|<-\\.->|<==>|-->|---|-\\.->|-\\.-|==>|===)(?:\\|[^|]*\\|)?\\s+)`, 'g');
          for (const token of line.matchAll(tokens)) {
            if (token[1]) continue;
            const text = token[0];
            const nodeId = text.match(/^[A-Za-z_][A-Za-z_0-9]*/)![0];
            if (text !== nodeId && flowNodes.has(nodeId)) accepted = false;
            if (groupId && flowNodes.has(nodeId) && nodeGroups.get(nodeId) !== groupId) accepted = false;
            if (!flowNodes.has(nodeId)) nodeGroups.set(nodeId, groupId);
            flowNodes.add(nodeId);
          }
        }
      }
    }
    if (type === 'state') {
      if (stateTransition.test(line)) {
        const ids = line.split(/\s+-->/)[0]!;
        stateIds.add(ids.trim()); stateIds.add(line.split('-->')[1]!.trim().split(/[\s:]/)[0]!);
        accepted = stateContent = true;
      } else if (stateLabel.test(line)) {
        const stateId = line.startsWith('state ') ? line.split(/\s+as\s+/)[1]! : line.split(':')[0]!.trim();
        // Upstream ignores a description after the state was referenced. Do not silently discard it.
        accepted = !stateIds.has(stateId); stateIds.add(stateId); stateContent = true;
      } else if (/^direction (?:TD|TB|LR|BT|RL)$/.test(line)) {accepted = !stateDirection; stateDirection = true;}
    }
    if (type === 'sequence') {
      const declaration = line.match(participant), msg = line.match(message);
      if (declaration) {
        const actor = declaration[2]!;
        if (declaredActors.has(actor) || usedActors.has(actor)) return fail('unsupported-syntax', 'Declare each participant once, before its messages.');
        declaredActors.add(actor); accepted = true;
      } else if (msg) {messages++; usedActors.add(msg[1]!); usedActors.add(msg[3]!); accepted = !/^(?:loop|alt|opt|par|critical|break|rect|else|and)/.test(msg[1]!);}
      else if (note.test(line)) {
        const actorText = line.replace(/^Note\s+(?:left of|right of|over)\s+/i, '').split(':')[0]!;
        const actors = actorText.split(',').map(value => value.trim());
        accepted = messages > 0 && (/^Note\s+over\s/i.test(line) || actors.length === 1);
        for (const actor of actors) usedActors.add(actor);
      }
      else if (/^(loop|alt|opt|par)(?:\s+.+)?$/.test(line)) {blocks.push(line.split(/\s+/)[0]!); accepted = blocks.length <= 8;}
      else if (/^else(?:\s+.+)?$/.test(line)) accepted = blocks.at(-1) === 'alt';
      else if (/^and(?:\s+.+)?$/.test(line)) accepted = blocks.at(-1) === 'par';
      else if (line === 'end') accepted = blocks.pop() !== undefined;
    }
    if (type === 'class' || type === 'er') {
      if (body) {
        if (line === '}') {body = false; accepted = true;}
        else accepted = (type === 'class' ? member : erAttribute).test(line);
      } else if (type === 'class') {
        if (classDeclaration.test(line)) {body = line.endsWith('{'); accepted = true;}
        else if (classRelation.test(line)) accepted = true;
        else {const inline = line.match(new RegExp(`^${id}\\s*:\\s*(.+)$`)); accepted = !!inline && member.test(inline[1]!);}
      } else if (new RegExp(`^${id}\\s+\\{$`).test(line)) {body = true; accepted = true;}
      else accepted = erRelation.test(line);
    }
    if (type === 'xychart') {
      const key = line.split(/\s+/)[0]!;
      const range = line.match(axisRange);
      const categories = line.match(/^x-axis\s+(?:"[^"<>]+"\s+)?\[([^\]]+)\]$/);
      const series = line.match(/^(bar|line)\s+\[([^\]]+)\]$/);
      if (series && numericList.test(series[2]!)) {
        const values = series[2]!.split(',').map(Number);
        accepted = values.every(value => Number.isFinite(value) && Math.abs(value) <= 1e9) && values.length <= 250 && (chartPoints === undefined || chartPoints === values.length);
        chartPoints = values.length; seriesCount++;
        // Multiple series have no Mermaid legend. Keep v1 honest and theme-native with one series.
        accepted = accepted && seriesCount === 1;
      } else if (!chartKeys.has(key)) {
        if (range) accepted = Number(range[2]) < Number(range[3]) && Math.abs(Number(range[2])) <= 1e9 && Math.abs(Number(range[3])) <= 1e9;
        else if (categories) {
          const labels = categories[1]!.split(',').map(value => value.trim());
          accepted = labels.every(value => /^[^"<>]+$/.test(value)) && (chartPoints === undefined || chartPoints === labels.length);
          chartPoints = labels.length;
        } else accepted = /^title\s+"[^"<>]+"$/.test(line) || /^y-axis\s+"[^"<>]+"$/.test(line);
        chartKeys.add(key);
      }
    }
    if (!accepted) return fail('unsupported-syntax', 'This statement is outside the supported Mermaid subset. Showing the original source.');
  }
  if (body || group || (type === 'flowchart' && !graphContent) || blocks.length || (type === 'state' && !stateContent) || (type === 'sequence' && !declaredActors.size && !usedActors.size) || (type === 'xychart' && !seriesCount)) return fail('invalid-syntax', 'The diagram is incomplete or has unclosed blocks.');
  if (type === 'flowchart' && groupIds.size) {
    for (const line of lines) {
      if (line.startsWith('subgraph ') || line === 'end') continue;
      for (const groupId of groupIds) if (new RegExp(`(?:^|\\s)${groupId}(?=[\\s\\[({]|$)`).test(line)) return fail('unsupported-syntax', 'Edges to subgraphs and group/node ID collisions are not supported.');
    }
  }
  return {ok: true, type};
}

/** Adapter for this exact release, not a general SVG sanitizer. The only consumer is an isolated img. */
export function prepareMermaidSVG(raw: string, palette: MermaidPalette, fontCSS = ''): MermaidImage {
  validateMermaidPalette(palette);
  if (mermaidBytes(raw) > mermaidLimits.outputBytes) throw new MermaidRenderError('output-limit', 'The diagram image exceeds 1 MiB.');
  let svg = raw.replace(/^\s*@import url\('https:\/\/fonts\.googleapis\.com\/[^']*'\);$/gm, '');
  const allowedTags = new Set(['svg', 'style', 'defs', 'marker', 'polygon', 'polyline', 'path', 'line', 'rect', 'text', 'tspan', 'circle', 'g', 'ellipse', 'title', 'desc', 'clipPath']);
  for (const match of svg.matchAll(/<\/?([\w:-]+)\b/g)) {
    if (!allowedTags.has(match[1]!)) throw new MermaidRenderError('render-failed', 'The renderer returned unsupported image markup.');
  }
  if (/<!|<\?|@import|@font-face|\b(?:href|src|on\w+)\s*=|url\((?!#[\w-]+\))/i.test(svg)) throw new MermaidRenderError('render-failed', 'The renderer returned an external or active image resource.');
  const root = svg.match(/^<svg\b[^>]+>/)?.[0] ?? '';
  const width = Number(root.match(/\bwidth="([\d.]+)"/)?.[1]) * palette.fontSize / 13;
  const height = Number(root.match(/\bheight="([\d.]+)"/)?.[1]) * palette.fontSize / 13;
  if (![width, height].every(value => Number.isFinite(value) && value > 0 && value <= mermaidLimits.dimension) || width * height > mermaidLimits.pixels) throw new MermaidRenderError('output-limit', 'The diagram dimensions exceed the image budget.');
  svg = svg.replace(root, root.replace(/\bwidth="[\d.]+"/, `width="${width}"`).replace(/\bheight="[\d.]+"/, `height="${height}"`));
  const family = palette.font === 'inter' ? "'Whip Diagram Inter', system-ui, sans-serif" : 'system-ui, sans-serif';
  svg = svg.replace(/font-family: [^;]+;/g, `font-family: ${family};`);
  // Meaningful secondary text/strokes must not bypass the resolved high-contrast palette.
  svg = svg.replace(/--_text-faint:\s*[^;]+;/g, '--_text-faint:var(--muted);')
    .replace(/--_inner-stroke:\s*[^;]+;/g, '--_inner-stroke:var(--border);')
    .replace(/--_(group-hdr|key-badge):\s*[^;]+;/g, '--_$1:var(--surface);');
  // Font CSS is built only from bundled font assets in the worker, never from source or theme strings.
  svg = svg.replace('<style>', `<style>${palette.font === 'inter' ? fontCSS : ''}`);
  if (mermaidBytes(svg) > mermaidLimits.outputBytes) throw new MermaidRenderError('output-limit', 'The diagram image exceeds 1 MiB including fonts.');
  return {svg, width, height};
}
