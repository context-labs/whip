import { fireEvent, render } from '@testing-library/react-native';
import type { Tree } from '@whip/sdk';
import { SessionMenu } from './session-menu';
let mockRuntime: any; const mockPin = jest.fn(async () => {});
jest.mock('../runtime/context', () => ({ useRuntime: () => mockRuntime, useRuntimeState: () => mockRuntime.getSnapshot() }));
jest.mock('../runtime/workspace-context', () => ({ useWorkspace: () => ({ pin: mockPin }), useWorkspaceState: () => ({ pins: [] }) }));
jest.mock('@expo/ui', () => ({ BottomSheet: () => null, RNHostView: require('react-native').View }));
function fixture() {
  const update = jest.fn(async () => ({})); const client = { runtimeID: 'runtime', trees: { update } };
  mockRuntime = { getSnapshot: () => ({ ready: true, client, host: { runtimeId: 'runtime' } }), requireReady: () => client, query: { invalidateQueries: async () => {} } };
  const tree = { id: 'tree', revision: '9007199254740993', metadata: { title: 'Title', archived: false, pinned: false } } as Tree;
  const close = jest.fn(); return { update, close, tree: () => <SessionMenu rootId="root" tree={tree} onDetails={() => {}} onClose={close} /> };
}
test('archive uses exact tree revision while pin stays local to root and runtime', async () => {
  const f = fixture(); const screen = await render(f.tree()); await fireEvent.press(screen.getByText('Pin on this phone')); expect(mockPin).toHaveBeenCalledWith('runtime', 'root', true); expect(f.update).not.toHaveBeenCalled();
  await fireEvent.press(screen.getByText('Archive session')); expect(f.update).toHaveBeenCalledWith('tree', '9007199254740993', { title: 'Title', archived: true, pinned: false });
});
test('uncertain rename preserves the title and requires fresh inspection before another mutation', async () => {
  const f = fixture(); f.update.mockRejectedValueOnce(new Error('Lost response')); const screen = await render(f.tree()); await fireEvent.press(screen.getByText('Rename')); await fireEvent.changeText(screen.getByLabelText('Session name'), 'Keep title'); await fireEvent.press(screen.getByText('Save name'));
  expect(f.close).not.toHaveBeenCalled(); expect(screen.getByDisplayValue('Keep title')).toBeTruthy(); expect(screen.getByRole('button', { name: 'Save name' })).toBeDisabled(); expect(f.update).toHaveBeenCalledTimes(1);
});
