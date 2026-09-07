import {test} from 'node:test';
import assert from 'node:assert/strict';
import {boundedCode, codeTokenStyle} from '../src/code-data.ts';
import {highlightCode} from '../src/code-highlight.ts';
import {themeCatalog} from '../src/generated/theme-catalog.ts';

const fixtures = {
  starlark: 'def inspect(name):\n    # read without changing the mailbox\n    return agents.get(name="explore", limit=12)\n',
  python: 'def run(value: str):\n    return {"done": True, "value": value}\n',
  javascript: 'const value = "<img src=x onerror=alert(1)>";\nconsole.log(value);',
  typescript: 'interface Agent { id: string; budget: number }\nconst agent: Agent = {id: "root", budget: 10};',
  go: 'package main\nfunc main() { println("hello", 12) }',
  json: '{"root_id":"root","running":true,"budget":12}',
  shell: '# inspect only\nprintf "%s\\n" "$WHIP_HOME"',
};
test('each supported language preserves exact text through AST tokenization', () => {
  for (const [language, code] of Object.entries(fixtures)) {
    const result = highlightCode(code, language);
    assert.equal(result.unavailable, undefined, language);
    assert.equal(result.tokens.map(token => token.text).join(''), code, language);
    assert.ok(result.tokens.some(token => token.kind !== 'plain'), language);
  }
});
test('unknown and absent languages remain inert plain text', () => {
  const code = '<script>alert("never execute")</script>';
  assert.deepEqual(highlightCode(code).tokens, [{text: code, kind: 'plain', offset: 0}]);
  assert.match(highlightCode(code, 'unknown').unavailable!, /Showing plain text/);
});
test('large bodies are bounded in bytes without splitting Unicode characters', () => {
  const source = '🙂'.repeat(10000);
  const bounded = boundedCode(source, 101);
  assert.equal(bounded.text, '🙂'.repeat(25));
  assert.equal(bounded.bytes, 100);
  assert.equal(bounded.truncated, true);
  assert.ok(boundedCode(source, Number.MAX_SAFE_INTEGER).bytes <= 16384);
});
test('token budgets fall back explicitly and do not expand into unbounded DOM', () => {
  const result = highlightCode('x+'.repeat(8192), 'javascript');
  assert.ok(result.tokens.length <= 4096);
  assert.match(result.unavailable!, /limits/);
  assert.equal(result.tokens.map(token => token.text).join(''), 'x+'.repeat(8192));
});
test('Chroma overrides retain full token styling when mapping Prism tokens', () => {
  const source = themeCatalog[0]!;
  const expected = {color: '#abcdef', background: '#123456', bold: true, italic: true, underline: true};
  const theme = {...source, code: {...source.code, tokens: {...source.code.tokens, NameFunction: expected}}};
  assert.deepEqual(codeTokenStyle(theme, 'function'), expected);
});
