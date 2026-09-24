import { render } from '@testing-library/react-native';
import SessionRoute from '../app/session/[rootId]';
let mockParams: any; const mockLookup = jest.fn(); const mockScope = jest.fn();
jest.mock('expo-router', () => ({ Stack: { Screen: () => null }, router: { back() {}, push() {} }, useIsFocused: () => true, useLocalSearchParams: () => mockParams }));
jest.mock('../runtime/workspace-context', () => ({ useWorkspace: () => ({ sessionRuntime: mockLookup }), useWorkspaceState: () => ({}) }));
jest.mock('../runtime/context', () => ({ RuntimeScope: ({ runtime }: any) => { mockScope(runtime); return null; } }));
jest.mock('react-native-keyboard-controller', () => ({ KeyboardAvoidingView: require('react-native').View }));
jest.mock('react-native-safe-area-context', () => ({ useSafeAreaInsets: () => ({ top: 0, bottom: 0 }) }));
jest.mock('../components/conversation', () => ({ ConversationRow: () => null, BodyInspector: () => null }));
jest.mock('@expo/ui', () => ({ BottomSheet: () => null, RNHostView: require('react-native').View }));
test('session leaf resolves the query identity, including identical root IDs on different hosts', async () => {
  const a = {}; const b = {}; mockLookup.mockImplementation((runtimeId: string) => runtimeId === 'a' ? a : b);
  mockParams = { rootId: 'same', runtimeId: 'a', hostId: 'profile-a' }; const screen = await render(<SessionRoute />);
  expect(mockLookup).toHaveBeenLastCalledWith('a', 'profile-a'); expect(mockScope).toHaveBeenLastCalledWith(a);
  mockParams = { rootId: 'same', runtimeId: 'b' }; await screen.rerender(<SessionRoute />);
  expect(mockLookup).toHaveBeenLastCalledWith('b', undefined); expect(mockScope).toHaveBeenLastCalledWith(b);
});
test('an ambiguous or missing runtime parameter cannot mount a host command scope', async () => {
  mockScope.mockClear(); mockLookup.mockClear(); mockParams = { rootId: 'root', runtimeId: ['a', 'b'] };
  const screen = await render(<SessionRoute />); expect(screen.getByText('Session link unavailable')).toBeTruthy();
  expect(mockLookup).not.toHaveBeenCalled(); expect(mockScope).not.toHaveBeenCalled();
});
