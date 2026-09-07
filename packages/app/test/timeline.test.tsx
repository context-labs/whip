import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { HistoryView } from '@whip/sdk/state';
import { executionCode, Prose, Timeline, timelineRows } from '../src/timeline';

const history = (messages: HistoryView['messages']): HistoryView => ({
  revision: '9007199254740993',
  throughSeq: 10,
  nextSeq: -1,
  hasMore: false,
  loading: false,
  messages,
  truncated: false,
});
describe('conversation presentation', () => {
  it('keeps identical authored messages separate and groups only adjacent runtime digests', () => {
    const entries = [
      { seq: 0, message: { role: 'user', content: 'Again', authored: true } },
      { seq: 1, message: { role: 'user', content: 'Again', authored: true } },
      { seq: 2, message: { role: 'user', content: 'Mailbox digest: waiting' } },
      { seq: 3, message: { role: 'user', content: 'Mailbox digest: waiting' } },
      {
        seq: 4,
        message: { role: 'user', content: 'Mailbox digest: different' },
      },
    ];
    const rows = timelineRows(history(entries), null);
    expect(rows).toHaveLength(4);
    expect(rows[0]!.id).not.toEqual(rows[1]!.id);
    expect(rows[2]!.deliveries).toBe(2);
    expect(rows[0]!.id).toContain('9007199254740993');
  });
  it('replaces cumulative call arguments and output without repeating messages', () => {
    const rows = timelineRows(undefined, [
      {
        seq: '1',
        kind: 'stream.tool.call',
        payload: { id: 'a', name: 'rlm_exec', args: '{"code":"print(' },
      },
      {
        seq: '2',
        kind: 'stream.tool.call',
        payload: { id: 'b', name: 'rlm_exec', args: '{"code":"print(2)"}' },
      },
      {
        seq: '3',
        kind: 'stream.tool.call',
        payload: { id: 'a', args: '{"code":"print(1)"}' },
      },
      {
        seq: '4',
        kind: 'stream.tool.completed',
        payload: { id: 'a', result: '1' },
      },
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({
      text: '1',
      args: '{"code":"print(1)"}',
      live: false,
    });
    expect(executionCode(rows[0]!.args!)).toBe('print(1)');
  });
  it('attaches persisted tool output to its call identity', () => {
    const rows = timelineRows(
      history([
        {
          seq: 0,
          message: {
            role: 'assistant',
            content: '',
            tool_calls: [
              {
                id: 'call',
                type: 'function',
                function: {
                  name: 'rlm_exec',
                  arguments: '{"code":"print(1)"}',
                },
              },
            ],
          },
        },
        {
          seq: 1,
          message: { role: 'tool', content: '1', tool_call_id: 'call' },
        },
      ]),
      null,
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ role: 'tool', text: '1' });
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

it('renders multipart text and image evidence without flattening it into duplicate messages', () => {
  const content = [
    { type: 'text', text: 'Please inspect' },
    {
      type: 'image_url',
      image_url: { url: 'data:image/png;base64,AAAA' },
      w: 1,
      h: 1,
    },
    { type: 'text', text: 'an attached note' },
  ];
  const rows = timelineRows(
    history([{ seq: 7, message: { role: 'user', content, authored: true } }]),
    null,
  );
  expect(rows).toHaveLength(1);
  expect(rows[0]?.text).toBe('Please inspect\n\nan attached note');
  expect(rows[0]?.images).toEqual([
    { url: 'data:image/png;base64,AAAA', width: 1, height: 1 },
  ]);
});

it('keeps image evidence when a multimodal tool reply merges into its call', () => {
  const rows = timelineRows(history([
    { seq: 1, message: { role: 'assistant', content: '', tool_calls: [{ id: 'image', type: 'function', function: { name: 'computer', arguments: '{}' } }] } },
    { seq: 2, message: { role: 'tool', tool_call_id: 'image', content: [{ type: 'text', text: 'Screenshot captured' }, { type: 'image_url', image_url: { url: 'data:image/png;base64,AAAA' } }] } },
  ]), null);
  expect(rows).toHaveLength(1);
  expect(rows[0]?.text).toBe('Screenshot captured');
  expect(rows[0]?.images).toHaveLength(1);
});
