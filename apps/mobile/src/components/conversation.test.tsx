import { act, fireEvent, render } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import type { TimelineRow } from '@whip/app/presentation';
import { BodyInspector, ConversationRow } from './conversation';
import { TEXT_PAGE_SIZE, TEXT_PREVIEW_SIZE } from './paged-text';

const mockReport = jest.fn();
const mockReadJSON = jest.fn(async () => ({}));
const mockContent = jest.fn(() => ({ readJSON: mockReadJSON }));
const mockUseQuery = jest.fn();
jest.mock('../runtime/context', () => ({ useRuntime: () => ({ report: mockReport }),
  useRuntimeState: () => ({ client: { content: mockContent }, host: { runtimeId: 'runtime' }, ready: true, active: true }) }));
jest.mock('@tanstack/react-query', () => ({ useQuery: (options: unknown) => mockUseQuery(options) }));
jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn(async () => {}) }));
jest.mock('../theme/theme', () => jest.requireActual('../theme/theme'));
jest.mock('@expo/ui', () => {
  const React = require('react'); const { View, Text, Pressable } = require('react-native');
  return { Host: View, Column: View, Button: ({ label, onPress }: { label: string; onPress(): void }) => React.createElement(Pressable, { accessibilityRole: 'button', onPress }, React.createElement(Text, {}, label)) };
});
jest.mock('./markdown', () => ({ Markdown: ({ text }: { text: string }) => require('react').createElement(require('react-native').Text, { testID: 'native-markdown' }, text) }));

beforeEach(() => { jest.clearAllMocks(); mockUseQuery.mockReturnValue({ data: undefined, isFetching: false }); });

test('collapsed and expanded tool arguments/output stay bounded while explicit Copy includes everything', async () => {
  const row: TimelineRow = { id: 'tool', role: 'tool', label: 'Tool', args: 'argument '.repeat(10_000), text: 'output '.repeat(10_000) };
  const screen = await render(<ConversationRow row={row} onInspect={jest.fn()} />);
  expect(screen.getByTestId('collapsed-message-preview').props.children.length).toBeLessThanOrEqual(TEXT_PREVIEW_SIZE);
  expect(mockUseQuery).not.toHaveBeenCalled(); expect(mockContent).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Show details'));
  expect(screen.getByTestId('source-text-page').props.children.length).toBeLessThanOrEqual(TEXT_PAGE_SIZE);
  expect(screen.queryByTestId('native-markdown')).toBeNull();
  for (const leaf of screen.getAllByText(/[\s\S]+/)) {
    if (typeof leaf.props.children === 'string') expect(leaf.props.children.length).toBeLessThanOrEqual(TEXT_PAGE_SIZE);
  }
  await fireEvent.press(screen.getByLabelText('Next text page'));
  await fireEvent.press(screen.getByLabelText('Copy message'));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(`${row.args}\n\n${row.text}`);
  await screen.rerender(<ConversationRow row={{ ...row, id: 'recycled', args: 'new arguments', text: '' }} onInspect={jest.fn()} />);
  expect(screen.getByTestId('collapsed-message-preview').props.children).toBe('new arguments');
  expect(screen.queryByTestId('source-text-page')).toBeNull();
});

test('a completed body-only tool says retained on host and opening stays explicit', async () => {
  const row = { id: 'retained', role: 'tool', text: '', body: { reference_id: 'body' } } as TimelineRow;
  const inspect = jest.fn();
  const screen = await render(<ConversationRow row={row} onInspect={inspect} />);
  expect(screen.getByText('Full message retained on host. Open full message to inspect.')).toBeOnTheScreen();
  expect(screen.queryByText('Waiting for output…')).toBeNull();
  expect(screen.getByLabelText('Copy message')).toBeDisabled();
  await fireEvent.press(screen.getByText('Show details'));
  expect(mockContent).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Open full message'));
  expect(inspect).toHaveBeenCalledWith(row);
});

test('explicit body inspection pages its bounded response, preserves full Copy and resets for a new body', async () => {
  const text = 'retained body '.repeat(12_000);
  mockUseQuery.mockReturnValue({ data: { content: text }, isFetching: false });
  const row = { id: 'retained', role: 'assistant', text: '', body: { reference_id: 'body' } } as TimelineRow;
  const screen = await render(<BodyInspector row={row} rootId="root" agentId="agent" />);
  expect(screen.getByTestId('source-text-page').props.children.length).toBeLessThanOrEqual(TEXT_PAGE_SIZE);
  const query = mockUseQuery.mock.calls[0][0];
  const signal = new AbortController().signal;
  await act(async () => { await query.queryFn({ signal }); });
  expect(mockReadJSON).toHaveBeenCalledWith({ maxBytes: 256 << 10, signal });
  await fireEvent.press(screen.getByLabelText('Next text page'));
  await fireEvent.press(screen.getByText('Copy message'));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(text);
  await screen.rerender(<BodyInspector row={{ ...row, body: { ...row.body!, reference_id: 'replacement' } }} rootId="root" agentId="agent" />);
  expect(screen.getByLabelText('Previous text page')).toBeDisabled();
});
