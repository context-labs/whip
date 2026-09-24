import { fireEvent, render } from '@testing-library/react-native';
import type { SessionView } from '@whip/sdk/state';
import { SessionMenu } from './session-menu';
let mockRuntime: any; let mockClient: any;
const mockPin = jest.fn(async () => {});
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('../runtime/workspace-context', () => ({ useWorkspace: () => ({ pin: mockPin }), useWorkspaceState: () => ({ pins: [] }) }));
jest.mock('@whip/sdk/react', () => ({ useSessionView: () => ({ status: 'live', root: { meta: { title: 'Title', archived: false } } }) }));
jest.mock('@expo/ui', () => ({ BottomSheet: () => null, RNHostView: require('react-native').View }));
function fixture() {
  mockClient = { supports: () => true }; const run = jest.fn(async () => ({ status: 'succeeded' }));
  mockRuntime = { getSnapshot: () => ({ ready: true, client: mockClient, host: { runtimeId: 'runtime' } }), requireReady: () => mockClient, run };
  const view = { session: { rootId: 'root', client: mockClient }, getSnapshot: () => ({ status: 'live', root: { meta: { archived: false } } }) } as unknown as SessionView;
  const close = jest.fn(); return { view, close, run, tree: () => <SessionMenu view={view} onDetails={() => {}} onClose={close} /> };
}
test('archive targets the viewed root while a pin stays local to its runtime identity', async () => {
  const f = fixture(); const screen = await render(f.tree()); await fireEvent.press(screen.getByText('Pin on this phone'));
  expect(mockPin).toHaveBeenCalledWith('runtime', 'root', true); expect(f.run).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Archive session'));
  expect(f.run).toHaveBeenCalledWith('session.archive', { archived: true }, { rootId: 'root' }); expect(f.close).toHaveBeenCalled();
});
test('a changed host rejects a rename at the tap and preserves the entered name', async () => {
  const f = fixture(); const screen = await render(f.tree()); await fireEvent.press(screen.getByText('Rename'));
  await fireEvent.changeText(screen.getByLabelText('Session name'), 'Keep this title'); mockRuntime.requireReady = () => ({});
  await fireEvent.press(screen.getByText('Save name')); expect(f.run).not.toHaveBeenCalled(); expect(f.close).not.toHaveBeenCalled();
  expect(screen.getByDisplayValue('Keep this title')).toBeTruthy(); expect(screen.getByText('Reconnect this host before changing the session.')).toBeTruthy();
});
