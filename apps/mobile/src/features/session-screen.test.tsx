import { act, fireEvent, render } from '@testing-library/react-native';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import type { MobileRuntime } from '../runtime/runtime';
import { SessionScreen } from '../app/session/[rootId]';

let mockRuntime: MobileRuntime;
let mockView: SessionView;
let mockSnapshot: SessionViewSnapshot;
let mockScrollOffset = 510;
const mockRows = [{ id: 'first', seq: '10', role: 'user' }, { id: 'middle', seq: '20', role: 'assistant' }, { id: 'last', seq: '30', role: 'assistant' }];
const mockScrollToIndex = jest.fn(async (_params: unknown) => {});
let mockMetadata: any;
jest.mock('@tanstack/react-query', () => ({ useQuery: (options: any) => ({ data: options.queryKey.includes('captured-reload') ? undefined : options.queryKey.includes('permission-mode') ? { tree_id: 'tree', mode: 'prompt', deny_interactive: false, revision: '1', updated_at: '2026-09-28T00:00:00Z' } : options.queryKey.includes('session-metadata') ? mockMetadata : options.queryKey.includes('inbox') ? { items: [] } : { inventory: { routes: [], defaults: null }, catalogs: [{ provider: 'provider', models: [{ id: 'changed', reasoning_efforts: ['low', 'high'] }] }] }, isFetching: false }) }));
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot(), useSessionOwner: () => ({ view: mockView, execution: {} }) }));
jest.mock('@whip/sdk/react', () => ({ useSessionView: () => mockSnapshot, useExecutionView: () => ({ turns: [], cells: [], output: null, operations: [] }) }));
jest.mock('@whip/app/presentation', () => ({ ...jest.requireActual('@whip/app/presentation'), conversationRows: () => mockRows }));
jest.mock('expo-router', () => ({ Stack: { Screen: () => null }, router: { setParams() {} }, useIsFocused: () => true,
  useLocalSearchParams: () => ({ rootId: 'root', runtimeId: 'runtime' }) }));
jest.mock('../components/conversation', () => ({ ConversationRow: () => null, BodyInspector: () => null }));
jest.mock('../components/requests', () => ({ Requests: () => null }));
jest.mock('react-native-keyboard-controller', () => ({ KeyboardAvoidingView: require('react-native').View }));
jest.mock('react-native-safe-area-context', () => ({ useSafeAreaInsets: () => ({ top: 0, right: 0, bottom: 0, left: 0 }) }));
jest.mock('../theme/theme', () => jest.requireActual('../theme/theme'));
jest.mock('@expo/ui', () => {
  const React = require('react'); const { View, Text, Pressable } = require('react-native');
  return { BottomSheet: ({ isPresented, children }: { isPresented: boolean; children: React.ReactNode }) => isPresented ? React.createElement(View, {}, children) : null, RNHostView: View, Host: View, Column: View,
    Button: ({ label, onPress, disabled }: { label: string; onPress(): void; disabled?: boolean }) => React.createElement(Pressable, { onPress, disabled }, React.createElement(Text, {}, label)) };
});
jest.mock('@shopify/flash-list', () => {
  const React = require('react'); const { View } = require('react-native');
  return { FlashList: React.forwardRef(({ onLoad, onScroll }: { onLoad(): void; onScroll: unknown }, ref: unknown) => {
    React.useImperativeHandle(ref, () => ({ getFirstVisibleIndex: () => 1, getLayout: () => ({ y: 430 }), getFirstItemOffset: () => 60,
      getAbsoluteLastScrollOffset: () => mockScrollOffset, scrollToIndex: mockScrollToIndex, scrollToEnd: () => {} }), []);
    React.useEffect(() => { onLoad(); }, []);
    return React.createElement(View, { testID: 'reading-list', onScroll });
  }) };
});

function fixture() {
  mockScrollOffset = 510; mockScrollToIndex.mockClear();
  mockSnapshot = { status: 'live', sessionID: 'root', runtimeID: 'runtime', unavailable: false, truncated: false, retainedBytes: 0, activity: { active_turn: null, queued_input_count: '0', pending_question_count: '0', pending_permission_count: '0' }, preview: null,
    history: { snapshot: { revision: '1', session_id: 'root', through_sequence: '30', message_count: '3' }, messages: [], gaps: [], olderCursor: null, latestMissing: false },
  } as unknown as SessionViewSnapshot;
  const configure = jest.fn(async () => ({}));
  const client = { runtimeID: 'runtime', session: () => ({ configure }) };
  const root = { id: 'root', parent_id: null, tree_id: 'tree', config_revision: '9007199254740993', lifecycle: 'active', working_directory: '/', definition: { id: 'assistant' }, configuration: { model: { name: 'model', provider: 'provider', effort: 'low' } } };
  mockMetadata = { root, recipient: root, tree: { id: 'tree', metadata: { title: 'Title', archived: false } } };
  const storage = { get: jest.fn(async () => ({ messageId: 'middle', seq: '20', revision: '1', offset: 20, follow: false })), set: jest.fn(async () => {}) };
  const inputs: readonly unknown[] = [];
  const run = jest.fn(async () => ({ status: 'succeeded' }));
  mockRuntime = { getSnapshot: () => ({ ready: true, active: true, host: { runtimeId: 'runtime', name: 'Host' }, commands: [], client }),
    draft: () => ({ text: '', revision: '' }), draftStatus: () => 'saved', isBlocked: () => false, query: { invalidateQueries: async () => {} }, report: jest.fn(), storage, requireReady: () => client, run,
    submitted: { subscribe: () => () => {}, getSnapshot: () => inputs, confirm() {} },
  } as unknown as MobileRuntime;
  mockView = { session: { rootId: 'root', client }, getSnapshot: () => mockSnapshot } as unknown as SessionView;
  return { storage, run, configure };
}

test('unmount saves the measured reading anchor before its native ref detaches', async () => {
  const f = fixture(); const screen = await render(<SessionScreen />);
  await act(async () => {});
  expect(mockScrollToIndex).toHaveBeenCalledWith({ index: 1, viewOffset: 20, animated: false });
  mockScrollOffset = 560;
  await fireEvent.scroll(screen.getByTestId('reading-list'), { nativeEvent: { contentOffset: { y: 560 }, contentSize: { height: 2000 }, layoutMeasurement: { height: 500 } } });
  await screen.unmount();
  expect(f.storage.set).toHaveBeenLastCalledWith('bookmarks', JSON.stringify(['runtime', 'root', 'root']), {
    messageId: 'middle', seq: '20', revision: '1', offset: 70, follow: false,
  });
});

test('a revision change while mounted restores again without applying the old pixel offset', async () => {
  fixture(); const screen = await render(<SessionScreen />); await act(async () => {});
  mockSnapshot = { ...mockSnapshot, history: { ...mockSnapshot.history, snapshot: { ...mockSnapshot.history.snapshot!, revision: '2' } } };
  await screen.rerender(<SessionScreen />); await act(async () => {});
  expect(mockScrollToIndex).toHaveBeenLastCalledWith({ index: 1, viewOffset: 0, animated: false });
  expect(screen.getByText('History changed. Showing the nearest retained message to your saved place.')).toBeTruthy();
});

test('a completed old restore cannot overwrite the new revision after history changes mid-scroll', async () => {
  const f = fixture(); let finish!: () => void;
  mockScrollToIndex.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  const screen = await render(<SessionScreen />); await act(async () => {});
  mockSnapshot = { ...mockSnapshot, history: { ...mockSnapshot.history, snapshot: { ...mockSnapshot.history.snapshot!, revision: '2' } } };
  await screen.rerender(<SessionScreen />); await act(async () => {});
  await act(async () => { finish(); });
  await screen.unmount();
  expect(f.storage.set).toHaveBeenLastCalledWith('bookmarks', expect.any(String), expect.objectContaining({ revision: '2' }));
});

test('model settings use configuration CAS and preserve exact revision without changing host defaults', async () => {
  const f = fixture(); const screen = await render(<SessionScreen />); await act(async () => {});
  await fireEvent.press(screen.getByLabelText('Session details')); await fireEvent.press(screen.getByText('Change model'));
  await fireEvent.press(screen.getByText('changed'));
  expect(f.configure).toHaveBeenCalledWith('9007199254740993', { model: { name: 'changed', provider: 'provider', effort: '' } });
  expect(f.run).not.toHaveBeenCalled();
});
