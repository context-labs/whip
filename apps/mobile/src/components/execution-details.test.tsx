import { fireEvent, render } from '@testing-library/react-native';
import type { CellExecutionRow } from '@whip/sdk/state';
import { MobileCell } from './execution-details';
import { nativeFixture } from '../../test/native-fixtures';
const mockCopy = jest.fn(async (_text: string) => {});
jest.mock('expo-clipboard', () => ({ setStringAsync: (text: string) => mockCopy(text) }));
jest.mock('../runtime/context', () => ({ useRuntime: () => ({ report: jest.fn() }) }));
jest.mock('./paged-text', () => ({ textPreview: (text: string) => text.slice(0, 512), PagedText: ({ text }: { text: string }) => { const { Text } = require('react-native'); return <Text>{text}</Text>; } }));
function fixture(): CellExecutionRow {
  const cell = { ...nativeFixture('Cell'), state: 'running' as const, result_message_id: null, finished_at: null };
  const preview = nativeFixture('CellOutput').preview!;
  return { cell, turn: null, call: null, result: null, operations: [], output: { ...preview, session_id: cell.session_id, turn_id: cell.turn_id, cell_id: cell.id, call_message_id: cell.call_message_id, call_id: cell.call_id, text: '{"result":{"output":"raw stdout"}}', truncated: true } };
}
test('mobile presents and copies exact provisional stdout, hides detached output and prefers committed results', async () => {
  const row = fixture(); const screen = await render(<MobileCell row={row} connected />);
  expect(screen.getByText('Live output · provisional')).toBeTruthy();
  expect(screen.getByText('Live output is truncated to the first 64 KiB.')).toBeTruthy();
  await fireEvent.press(screen.getByText('Copy output'));
  expect(mockCopy).toHaveBeenCalledWith(row.output!.text);
  await screen.rerender(<MobileCell row={row} connected={false} />);
  expect(screen.queryByText('Live output · provisional')).toBeNull();
  expect(screen.queryByText(row.output!.text)).toBeNull();
  const result = { call_id: row.cell.call_id, output: JSON.stringify({ result: { execution_engine: 'quickjs', output: 'committed stdout', has_value: true, value: null, metrics: { quickjs_jobs: 4 } } }), is_error: false };
  row.result = { message: nativeFixture('Message'), value: result };
  await screen.rerender(<MobileCell row={row} connected />);
  expect(screen.queryByText('Live output · provisional')).toBeNull(); expect(screen.getByText('committed stdout')).toBeTruthy();
  expect(screen.getByText('Return value')).toBeTruthy(); expect(screen.getByText('null')).toBeTruthy(); expect(screen.getByText('4 QuickJS jobs')).toBeTruthy();
});
