import {performance} from 'node:perf_hooks';
import {highlightCode} from '../src/code-highlight.ts';
const cases = [
  ['starlark', 'def inspect():\n    return agents.get(name="explore", limit=12)\n'.repeat(180)],
  ['typescript', 'const value: string = `${agent.id}: ${agent.budget}`;\n'.repeat(220)],
  ['javascript', '${('.repeat(5000)],
  ['python', '"""'.repeat(5000)],
  ['go', 'package main\nfunc main() { println("ok") }\n'.repeat(320)],
];
const results = [];
for (const [language, source] of cases) {
  const code = source.slice(0, 16384); const samples = [];
  for (let i = 0; i < 25; i++) {const start = performance.now(); highlightCode(code, language); samples.push(performance.now() - start);}
  samples.sort((a, b) => a - b);
  results.push({language, characters: code.length, medianMs: +samples[12].toFixed(2), p95Ms: +samples[23].toFixed(2), maxMs: +samples[24].toFixed(2)});
}
console.log(JSON.stringify({node: process.version, results}, null, 2));
