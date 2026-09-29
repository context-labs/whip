// Opt-in diagnostic only: the existing workload and production renderer remain
// unchanged. A traced run is excluded from latency and memory acceptance data.
import assert from 'node:assert/strict';
import { open, rename } from 'node:fs/promises';

export async function traceInput(context, page, path) {
  const session = await context.newCDPSession(page);
  let startingTimer, startingDetach;
  const usage = { samples: 0, maximumPercentFull: null };
  const onUsage = event => {
    if (!Number.isFinite(event.percentFull)) return;
    usage.samples++;
    usage.maximumPercentFull = Math.max(usage.maximumPercentFull ?? 0, event.percentFull);
  };
  session.on('Tracing.bufferUsage', onUsage);
  const starting = session.send('Tracing.start', { transferMode: 'ReturnAsStream', streamFormat: 'json', bufferUsageReportingInterval: 1000,
    traceConfig: { recordMode: 'recordUntilFull', traceBufferSizeInKb: 32768,
      includedCategories: ['devtools.timeline', 'blink.user_timing', 'disabled-by-default-devtools.timeline.stack', 'disabled-by-default-v8.cpu_profiler'] } });
  try {
    await Promise.race([starting, new Promise((_, reject) => {
      startingTimer = setTimeout(() => {
        startingDetach = session.detach();
        void startingDetach.catch(() => {});
        reject(new Error('Input trace startup exceeded 10 seconds'));
      }, 10000);
    })]);
  } catch (error) {
    session.off('Tracing.bufferUsage', onUsage);
    try { await (startingDetach ?? session.detach()); }
    finally { await starting.catch(() => {}); }
    throw error;
  } finally { clearTimeout(startingTimer); }
  let pending;
  return { finish() {
    return pending ??= (async () => {
      let timer, stream, file, complete, rejectComplete, detaching, timedOut = false;
      const detach = () => detaching ??= session.detach();
      const capture = (async () => {
        try {
          const completed = new Promise((resolve, reject) => { complete = resolve; rejectComplete = reject; session.once('Tracing.tracingComplete', complete); });
          void completed.catch(() => {});
          await session.send('Tracing.end');
          const result = await completed;
          stream = result.stream;
          assert(stream && result.traceFormat === 'json');
          file = await open(path + '.partial', 'wx');
          let bytes = 0, reads = 0;
          for (;;) {
            assert(++reads <= 2048, 'Input trace read count exceeded');
            const chunk = await session.send('IO.read', { handle: stream, size: 65536 });
            const data = Buffer.from(chunk.data, chunk.base64Encoded ? 'base64' : 'utf8');
            bytes += data.length; assert(bytes <= (64 << 20), 'Input trace exceeds 64 MiB');
            await file.writeFile(data);
            if (chunk.eof) break;
          }
          await session.send('IO.close', { handle: stream }); stream = undefined;
          await file.close(); file = undefined;
          assert.equal(result.dataLossOccurred, false, `Input trace reported lost data (maximum observed buffer fraction ${usage.maximumPercentFull ?? 'unobserved'}); incomplete diagnostic only: ${path}.partial`);
          assert.equal(timedOut, false, 'Input trace finish deadline elapsed');
          await rename(path + '.partial', path);
          return { path, bytes, bufferUsage: usage, browserBufferKiB: 32768, outputByteLimit: 64 << 20,
            boundary: 'Diagnostic DevTools layout/script trace of a bounded window within the existing 40-key input workload; tracing overhead excludes this run from latency and memory acceptance.' };
        } finally {
          try { if (stream && !detaching) await session.send('IO.close', { handle: stream }); }
          finally { await file?.close(); }
        }
      })();
      const expired = new Promise((_, reject) => {
        timer = setTimeout(() => {
          // Detachment rejects in-flight protocol calls. Join capture below so
          // neither a stalled end/read nor a file write outlives this owner.
          timedOut = true;
          const error = new Error('Input trace finish exceeded 10 seconds; partial output is diagnostic only');
          rejectComplete?.(error);
          void detach().catch(() => {});
          reject(error);
        }, 10000);
      });
      try { return await Promise.race([capture, expired]); }
      finally {
        clearTimeout(timer);
        session.off('Tracing.tracingComplete', complete);
        session.off('Tracing.bufferUsage', onUsage);
        try { await detach(); } finally { await capture.catch(() => {}); }
      }
    })();
  } };
}
