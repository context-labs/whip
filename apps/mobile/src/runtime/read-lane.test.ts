/** @jest-environment node */
import { ReadLane } from './read-lane';
test('caps concurrent index reads and drops an aborted queued read', async () => {
  const lane = new ReadLane(); const control = new AbortController(); let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; }); let active = 0; let peak = 0;
  const read = async () => { peak = Math.max(peak, ++active); await gate; active--; return 1; };
  const one = lane.run(new AbortController().signal, read); const two = lane.run(new AbortController().signal, read);
  const never = jest.fn(read); const cancelled = lane.run(control.signal, never); const rejection = expect(cancelled).rejects.toThrow('cancelled');
  const last = lane.run(new AbortController().signal, read); control.abort(); await rejection; release();
  expect(await Promise.all([one, two, last])).toEqual([1, 1, 1]); expect(peak).toBe(2); expect(never).not.toHaveBeenCalled();
});
