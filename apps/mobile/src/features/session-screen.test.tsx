import { act, fireEvent, render } from '@testing-library/react-native';
import type { SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import type { MobileRuntime } from '../runtime/runtime';
import SessionScreen from '../app/session/[rootId]';

let mockRuntime: MobileRuntime;
let mockView: SessionView;
let mockSnapshot: SessionViewSnapshot;
let mockScrollOffset = 510;
const mockRows = [{ id: 'first', seq: 10, role: 'user' }, { id: 'middle', seq: 20, role: 'assistant' }, { id: 'last', seq: 30, role: 'assistant' }];
const mockScrollToIndex = jest.fn(async (_params: unknown) => {});
jest.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: { result: { catalogs: { provider: { models: [{ id: 'changed', reasoning_efforts: ['low', 'high'] }] } } } }, isFetching: false }) }));
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot(), useRootView: () => mockView }));
jest.mock('@whip/sdk/react', () => ({ useSessionView: () => mockSnapshot }));
jest.mock('@whip/app/presentation', () => ({ ...jest.requireActual('@whip/app/presentation'), conversationRows: () => mockRows }));
jest.mock('expo-router', () => ({ Stack: { Screen: () => null }, router: { setParams() {} }, useIsFocused: () => true,
  useLocalSearchParams: () => ({ rootId: 'root', runtimeId: 'runtime' }) }));
jest.mock('../components/conversation', () => ({ ConversationRow: () => null, BodyInspector: () => null }));
jest.mock('../components/requests', () => ({ Requests: () => null }));
jest.mock('react-native-keyboard-controller', () => ({ KeyboardAvoidingView: require('react-native').View }));
jest.mock('react-native-safe-area-context', () => ({ useSafeAreaInsets: () => ({ top: 0, right: 0, bottom: 0, left: 0 }) }));
jest.mock('../theme/theme', () => ({ useTheme: () => ({ dark: true, colors: { foreground: '#fff', muted: '#aaa', panel: '#222', element: '#333', background: '#111', border: '#444', primary: '#55aaff', error: '#f55', hover: '#333' } }) }));
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
  mockSnapshot = { status: 'live', unavailable: false, truncated: false, collections: {}, retainedBytes: 0,
    root: { root_id: 'root', history_revision: '1', meta: { title: 'Title', model: 'model', provider: 'provider' }, active_turns: {}, agents: [], questions: [], permissions: [] },
    history: { root: { revision: '1', messages: [], loading: false, hasMore: false, truncated: false, throughSeq: 30, nextSeq: 0 } },
  } as unknown as SessionViewSnapshot;
  const client = { supports: () => true, getSnapshot: () => ({ info: { runtime_id: 'runtime' } }) };
  const storage = { get: jest.fn(async () => ({ messageId: 'middle', seq: 20, revision: '1', offset: 20, follow: false })), set: jest.fn(async () => {}) };
  const inputs: readonly unknown[] = [];
  const run = jest.fn(async () => ({ status: 'succeeded' }));
  mockRuntime = { getSnapshot: () => ({ ready: true, active: true, host: { runtimeId: 'runtime', name: 'Host' }, commands: [], client }),
    draft: () => ({ text: '', revision: '' }), draftStatus: () => 'saved', isBlocked: () => false, acquireAgent: () => ({ release() {} }), report: jest.fn(), storage, requireReady: () => client, run,
    submitted: { subscribe: () => () => {}, getSnapshot: () => inputs },
  } as unknown as MobileRuntime;
  mockView = { session: { rootId: 'root', client }, getSnapshot: () => mockSnapshot } as unknown as SessionView;
  return { storage, run };
}

test('unmount saves the measured reading anchor before its native ref detaches', async () => {
  const f = fixture(); const screen = await render(<SessionScreen />);
  await act(async () => {});
  expect(mockScrollToIndex).toHaveBeenCalledWith({ index: 1, viewOffset: 20, animated: false });
  mockScrollOffset = 560;
  await fireEvent.scroll(screen.getByTestId('reading-list'), { nativeEvent: { contentOffset: { y: 560 }, contentSize: { height: 2000 }, layoutMeasurement: { height: 500 } } });
  await screen.unmount();
  expect(f.storage.set).toHaveBeenLastCalledWith('bookmarks', JSON.stringify(['runtime', 'root', 'root']), {
    messageId: 'middle', seq: 20, revision: '1', offset: 70, follow: false,
  });
});

test('a revision change while mounted restores again without applying the old pixel offset', async () => {
  fixture(); const screen = await render(<SessionScreen />); await act(async () => {});
  mockSnapshot = { ...mockSnapshot, root: { ...mockSnapshot.root!, history_revision: '2' },
    history: { root: { ...mockSnapshot.history.root, revision: '2' } } };
  await screen.rerender(<SessionScreen />); await act(async () => {});
  expect(mockScrollToIndex).toHaveBeenLastCalledWith({ index: 1, viewOffset: 0, animated: false });
  expect(screen.getByText('History changed. Showing the nearest retained message to your saved place.')).toBeTruthy();
});

test('a completed old restore cannot overwrite the new revision after history changes mid-scroll', async () => {
  const f = fixture(); let finish!: () => void;
  mockScrollToIndex.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  const screen = await render(<SessionScreen />); await act(async () => {});
  mockSnapshot = { ...mockSnapshot, root: { ...mockSnapshot.root!, history_revision: '2' }, history: { root: { ...mockSnapshot.history.root, revision: '2' } } };
  await screen.rerender(<SessionScreen />); await act(async () => {});
  await act(async () => { finish(); });
  await screen.unmount();
  expect(f.storage.set).toHaveBeenLastCalledWith('bookmarks', expect.any(String), expect.objectContaining({ revision: '2' }));
});

test('model settings recheck active descendants at the tap and never persist a host default', async () => {
  const f = fixture(); const screen = await render(<SessionScreen />); await act(async () => {});
  await fireEvent.press(screen.getByLabelText('Session details'));
  await fireEvent.press(screen.getByText('Change model'));
  mockSnapshot = { ...mockSnapshot, root: { ...mockSnapshot.root!, active_turns: { child: 'turn' } } };
  await fireEvent.press(screen.getByText('changed'));
  expect(f.run).not.toHaveBeenCalled(); expect(mockRuntime.report).toHaveBeenCalled();
  mockSnapshot = { ...mockSnapshot, root: { ...mockSnapshot.root!, active_turns: {} } };
  await fireEvent.press(screen.getByText('changed'));
  expect(f.run).toHaveBeenCalledWith('session.model', { model: 'changed', provider: 'provider', persist_default: false }, { rootId: 'root', intent: { agentId: 'root' } });
  await fireEvent.press(screen.getByText('high'));
  expect(f.run).toHaveBeenLastCalledWith('session.effort', { effort: 'high', persist_default: false }, { rootId: 'root', intent: { agentId: 'root' } });
});
