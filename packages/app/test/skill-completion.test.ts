import { describe, expect, it } from 'vitest';
import { insertSkill, skillTrigger } from '../src/skill-completion';

describe('slash skill token parsing', () => {
  it.each([
    ['/', 1, { start: 0, end: 1, caret: 1, prefix: '' }],
    ['Use /pony please', 7, { start: 4, end: 9, caret: 7, prefix: 'po' }],
    ['first\n/pony', 11, { start: 6, end: 11, caret: 11, prefix: 'pony' }],
    ['🙂 /pony', 8, { start: 3, end: 8, caret: 8, prefix: 'pony' }],
  ])('uses native caret/range in %s', (text, caret, expected) => {
    expect(skillTrigger(text as string, caret as number)).toEqual(expected);
  });
  it.each(['https://pony', 'path/to/file', '/tmp/file', '/two/parts', '`/pony', '```go\n/pony', '~~~go\n/pony', 'some/pony', '$pony'])('ignores %s', text => {
    expect(skillTrigger(text, text.length)).toBeNull();
  });
  it.each(['`code` /pony', '```\ncode\n```\n/pony', '~~~\ncode\n~~~\n/pony', '``code ` inner`` /pony'])('accepts after closed code in %s', text => {
    expect(skillTrigger(text, text.length)?.prefix).toBe('pony');
  });
  it('rejects noncollapsed and invalid selections', () => {
    expect(skillTrigger('/pony', 1, 3)).toBeNull();
    expect(skillTrigger('/pony', 0)).toBeNull();
    expect(skillTrigger('/pony', 100)).toBeNull();
  });
  it('replaces the entire token, preserves surrounding whitespace and supports UTF-16', () => {
    const text = '🙂 /pony\nnext';
    expect(insertSkill(text, skillTrigger(text, 6)!, '$ponytail')).toEqual({ text: '🙂 $ponytail\nnext', caret: 13 });
    expect(insertSkill('/po', skillTrigger('/po', 3)!, '$ponytail')).toEqual({ text: '$ponytail ', caret: 10 });
  });
  it.each(['café', 'café', '技能', '🦄', 'pony!'])('filters and inserts invocable host name %s', name => {
    const text = '/' + name;
    const trigger = skillTrigger(text, text.length)!;
    expect(trigger.prefix).toBe(name);
    expect(insertSkill(text, trigger, '$' + name)).toEqual({ text: '$' + name + ' ', caret: name.length + 2 });
    expect(insertSkill('/', skillTrigger('/', 1)!, '$' + name)?.text).toBe('$' + name + ' ');
  });
  it('rejects stale ranges, invalid references and overlong drafts', () => {
    expect(insertSkill('/new', skillTrigger('/old', 4)!, '$ponytail')).toBeNull();
    expect(insertSkill('/po', skillTrigger('/po', 3)!, '/ponytail')).toBeNull();
    expect(insertSkill('/po', skillTrigger('/po', 3)!, '$two words')).toBeNull();
    const text = 'x'.repeat(256 * 1024 - 2) + ' /';
    expect(insertSkill(text, skillTrigger(text, text.length)!, '$ponytail')).toBeNull();
  });
});
it.each([' ', '\t', '\n'])('continues after a preserved separator %j without extending the reference', separator => {
  const text = `Review /po${separator}tomorrow`;
  const result = insertSkill(text, skillTrigger(text, 10)!, '$ponytail')!;
  expect(result.text).toBe(`Review $ponytail${separator}tomorrow`);
  expect(result.text.slice(0, result.caret) + 'carefully ' + result.text.slice(result.caret)).toBe(`Review $ponytail${separator}carefully tomorrow`);
});
