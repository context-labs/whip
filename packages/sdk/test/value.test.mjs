import assert from 'node:assert/strict';
import { test } from 'node:test';
import { bytesBase64, utf8Base64 } from '../dist/value.js';

function view(length) {
  const storage = new Uint8Array(length + 14).fill(0xa5);
  const body = storage.subarray(7, length + 7);
  for (let index = 0; index < body.length; index++) body[index] = index % 256;
  return body;
}

for (const length of [0, 1, 2, 3, 255, 256, 8191, 8192, 8193, 4 << 20]) {
  test(`portable encoding preserves the exact ${length}-byte view and standard padding`, () => {
    const body = view(length);
    Object.defineProperty(body, 'toBase64', { value: undefined });
    assert.equal(bytesBase64(body), Buffer.from(body).toString('base64'));
  });
}

test('native encoding receives the exact view without constructing a binary string', t => {
  const body = view(8193), expected = Buffer.from(body).toString('base64');
  let calls = 0;
  Object.defineProperty(body, 'toBase64', { value() { assert.equal(this, body); calls++; return expected; } });
  t.mock.method(globalThis, 'btoa', () => { throw new Error('Portable encoding should not run'); });
  assert.equal(bytesBase64(body), expected);
  assert.equal(calls, 1);
});

test('UTF-8 receipt encoding preserves Unicode and replacement semantics', () => {
  for (const text of ['', 'exact receipt', 'Nul\0bytes', 'é漢字𝄞🙂', '\ud800tail\udfff']) {
    assert.equal(utf8Base64(text), Buffer.from(text, 'utf8').toString('base64'));
  }
});
