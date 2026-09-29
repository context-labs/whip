// A passive sample by default. Forced collection is an explicit, separately
// labeled diagnostic, never the end-of-work acceptance sample.
export async function sampleBrowserRetention(context, page, { collectGarbage = false } = {}) {
  const session = await context.newCDPSession(page);
  let timer, detaching;
  const detach = () => detaching ??= session.detach();
  const capture = (async () => {
    if (collectGarbage) await session.send('HeapProfiler.collectGarbage');
    return { ...(await session.send('Runtime.getHeapUsage')),
      ...(await session.send('Memory.getDOMCounters')),
      collection: collectGarbage ? 'forced-diagnostic' : 'natural',
      boundary: collectGarbage
        ? 'Post-forced-GC diagnostic only; does not establish natural retention or memory acceptance.'
        : 'Passive sequential end-of-work heap/DOM sample before any requested collection; not a peak or physical-footprint measurement.',
    };
  })();
  const expired = new Promise((_, reject) => {
    timer = setTimeout(() => {
      void detach().catch(() => {});
      reject(new Error('Browser retention sampling exceeded 10 seconds'));
    }, 10000);
  });
  try { return await Promise.race([capture, expired]); }
  finally {
    clearTimeout(timer);
    try { await detach(); } finally { await capture.catch(() => {}); }
  }
}
