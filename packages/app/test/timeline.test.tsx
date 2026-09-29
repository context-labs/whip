import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { Message } from '@whip/sdk';
import { executionCode, Prose, Timeline, timelineRows } from '../src/timeline';
import { responseCopies } from '../src/chat-activity-rows';
import { history, messageBase } from './native-conversation-fixture';

const authored = (
  id: string,
  sequence: string,
  parts: Extract<Message, { role: 'user' }>['parts'],
): Extract<Message, { role: 'user' }> => ({
  ...messageBase,
  id,
  sequence,
  role: 'user',
  opening_input: true,
  input_id: 'input-' + id,
  parts,
});
describe('conversation presentation', () => {
  it('keeps identical authored messages and distinct mail identities separate', () => {
    const first = authored('first', '9007199254740993', [
      { type: 'text', text: 'Again' },
    ]);
    const second = authored('second', '9007199254740994', first.parts);
    const mail: Message = {
      ...first,
      id: 'mail',
      sequence: '9007199254740995',
      input_id: null,
      mail: { id: 'delivery', revision: '1', presentation: 'digest' },
    };
    const rows = timelineRows(history([first, second, mail]));
    expect(rows).toHaveLength(3);
    expect(rows.map((row) => row.id)).toEqual([
      'message:first',
      'message:second',
      'message:mail',
    ]);
    expect(rows.map((row) => row.role)).toEqual(['user', 'user', 'mailbox']);
    expect(rows[0]?.seq).toBe('9007199254740993');
  });
  it('replaces a provisional call wholesale and attaches committed output by exact call identity', () => {
    const preview = {
      attempt_id: 'attempt',
      message_id: 'answer',
      turn_id: 'turn',
      revision: '1',
      reasoning: '',
      text: '',
      truncated: false,
      calls: [
        { index: 0, id: 'call', name: 'execute', arguments: '{"code":"print(' },
      ],
    };
    const initial = timelineRows(undefined, preview);
    const replacement = timelineRows(undefined, {
      ...preview,
      revision: '2',
      calls: [{ ...preview.calls[0]!, arguments: '{"code":"print(1)"}' }],
    });
    expect(initial[0]?.id).toBe(replacement[0]?.id);
    expect(replacement).toHaveLength(1);
    expect(executionCode(replacement[0]!.args!)).toBe('print(1)');
    const call: Message = {
      ...messageBase,
      id: 'answer',
      sequence: '1',
      role: 'assistant',
      parts: [
        {
          type: 'tool_call',
          call: {
            id: 'call',
            name: 'execute',
            arguments: { code: 'print(1)' },
          },
        },
      ],
    };
    const result: Message = {
      ...messageBase,
      id: 'result',
      sequence: '2',
      role: 'tool',
      parts: [
        {
          type: 'tool_result',
          result: { call_id: 'call', output: '1', is_error: false },
        },
      ],
    };
    const committed = timelineRows(history([call, result]), preview);
    expect(committed).toHaveLength(1);
    expect(committed[0]).toMatchObject({
      role: 'tool',
      text: '1',
      args: '{"code":"print(1)"}',
    });
    expect(committed[0]?.live).not.toBe(true);
  });
  it('renders untrusted Markdown without executable HTML or remote image loads', () => {
    const { container } = render(
      <Prose
        text={
          '<script>alert(1)</script>\n\n[link](javascript:alert(1))\n\n![image](https://example.com/image.png)\n\n**Hello**'
        }
      />,
    );
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('a[href^="javascript:"]')).toBeNull();
    expect(container.querySelector('strong')?.textContent).toBe('Hello');
  });
});

it('the conversation scroll viewport is a named, keyboard-focusable region', () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  try {
    render(
      <RuntimeContext.Provider
        value={{ report: vi.fn() } as unknown as AppRuntime}
      >
        <Timeline
          rows={[]}
          hasMore={false}
          loadOlder={async () => {}}
          readBody={() => {}}
        />
      </RuntimeContext.Provider>,
    );
    const viewport = screen.getByRole('region', { name: 'Conversation' });
    expect(viewport.tabIndex).toBe(0);
    viewport.focus();
    expect(document.activeElement).toBe(viewport);
  } finally {
    vi.unstubAllGlobals();
  }
});

it('retains scoped content references beside multipart text without manufacturing image URLs', () => {
  const rows = timelineRows(
    history([
      authored('input', '7', [
        { type: 'text', text: 'Please inspect' },
        { type: 'content', reference_id: 'image' },
        { type: 'text', text: 'an attached note' },
      ]),
    ]),
  );
  expect(rows).toHaveLength(1);
  expect(rows[0]?.text).toBe('Please inspect\n\nan attached note');
  expect(rows[0]?.references).toEqual(['image']);
  expect(rows[0]?.images).toBeUndefined();
});
it('keeps scoped image evidence when a multimodal tool reply joins its exact call', () => {
  const call: Message = {
    ...messageBase,
    id: 'call',
    sequence: '1',
    role: 'assistant',
    parts: [
      {
        type: 'tool_call',
        call: { id: 'image', name: 'execute', arguments: {} },
      },
    ],
  };
  const result: Message = {
    ...messageBase,
    id: 'result',
    sequence: '2',
    role: 'tool',
    parts: [
      {
        type: 'tool_result',
        result: {
          call_id: 'image',
          output: 'Screenshot captured',
          is_error: false,
        },
      },
      { type: 'content', reference_id: 'image' },
    ],
  };
  const rows = timelineRows(history([call, result]));
  expect(rows).toHaveLength(1);
  expect(rows[0]?.text).toBe('Screenshot captured');
  expect(rows[0]?.references).toEqual(['image']);
});
it('uses canonical admission provenance to distinguish authored attachments from internal deliveries', () => {
  const input = authored('input', '1', [
    { type: 'text', text: 'images attached:' },
    { type: 'content', reference_id: 'image' },
  ]);
  const first: Message = {
    ...messageBase,
    id: 'first',
    sequence: '2',
    role: 'assistant',
    parts: [{ type: 'text', text: 'Taking a screenshot.' }],
  };
  const internal: Message = {
    ...input,
    id: 'internal',
    sequence: '3',
    input_id: null,
    opening_input: false,
  };
  const last: Message = {
    ...first,
    id: 'last',
    sequence: '4',
    parts: [{ type: 'text', text: 'Here is what I found.' }],
  };
  const rows = timelineRows(history([input, first, internal, last]));
  expect(rows.map((row) => row.role)).toEqual([
    'user',
    'assistant',
    'internal',
    'assistant',
  ]);
  expect(rows[2]?.references).toEqual(['image']);
  expect([...responseCopies(rows, false).values()]).toEqual([
    {
      text: 'Taking a screenshot.\n\nHere is what I found.',
      label: 'Copy response',
    },
  ]);
});
