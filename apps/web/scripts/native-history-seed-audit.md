# Native history seed timeout diagnostics

## Observed failure

The hosted PR275 browser job and PR276 Settings job exceeded the seed helper's
unchanged 120-second context. The Node process wrapper retains its independent
125-second bound. Compilation and runtime startup occur before the seed process
begins and do not consume this context.

In PR276, the last completed scenario was Chromium REPL. The next scenario is
Chromium stored-body history, not Firefox: the runner executes both modes for
one engine before starting the other. The former log omitted those startup
boundaries, and contained only `context deadline exceeded` on failure.

Previous isolated profiling found substantial SQL preparation and durable-write
cost but did not reproduce the hosted Ubuntu/amd64 timeout. A proposed cache
change gave no measured benefit and was discarded. This leaf adds evidence for
a fresh hosted run. It is not a claimed timeout fix.

## Bounded evidence

The existing Go helper emits JSON diagnostics to stderr, leaving its successful
stdout evidence unchanged. Each process emits at most 64 records, reserving the
last record for completion/failure. The successful path currently emits 37
records, including ownership, store opening, root validation, content, root
pairs, cell history, children, final history verification and store closure.
Root progress is emitted every 500 completed pairs; child progress every 10
completed children. A failure reports the exact last completed count, even
between progress intervals. Reports include total and stage elapsed time,
process PID, cumulative user/system CPU time and the operating system's output
block-operation count. That count is not a byte count or an fsync-latency
measurement, and a zero value on macOS does not establish zero I/O.

There are no diagnostic timers, goroutines, database queries, retries, message
bodies, credentials or directory scans. Error classifications are bounded names;
the progress report never copies error text. The wrapper forwards stderr while
the child is alive under its existing 128KiB limit, then records actual child
exit code/signal and elapsed time after the existing process promise settles.
The live-owner rejection probe is identified separately from the real seed.

Before seeding, the wrapper records the exact owned runtime PID/epoch and
observed exit. Settings logs engine/mode startup plus browser-close and runtime
exit before advancing to the next scenario. The existing cleanup already awaits
`browser.close()` and then fixture closure, which joins its executor/runtime and
provider before removing its disposable directory. These logs make that order
visible; this review found no evidence that an earlier owned runtime was left
running. They do not prove that a hosted runner has no unrelated CPU or I/O load.

All original 4,998 root pairs, the four-message cell turn, 100 children with 100
messages each, 128 operations, scoped 1.4MB content, selected compaction, ownership
checks and one-use marker remain unchanged. Normal store transactions and FULL
synchronous durability are unchanged. No deadline was raised, workload reduced,
synthetic SQL introduced or failed seed replayed.

## Validation

- Two focused Go diagnostic tests pass under the race detector (1.334s). They
  preserve the failure stage/count and suppress error bodies, and force the
  record bound while retaining a truthful terminal truncation marker.
- Focused Go vet, pinned golangci-lint 2.13.1 with Go 1.27.0 (zero issues), Linux
  amd64 compile, JavaScript syntax and diff checks pass.
- Both existing actual native-history fixture variants pass, 40.016s total.
  They retain exact root/child counts, scoped content, all 128 operations,
  compaction and fresh real provider execution/cancellation. The measured seed
  processes completed in 16.718s and 15.247s, with 8,129 and 8,130 stderr bytes.
  Both seeds explicitly followed the owned runtime's SIGKILL exit; both helper
  processes exited 0. The live-owner negative processes still exited 1.

Local passes are not an Ubuntu/amd64 hosted reproduction and do not establish a
performance improvement. The hosted timeout remains unresolved until its next
stage/count/elapsed evidence identifies a concrete cause.


Actual `settings-conversation.mjs` validation passed all four scenarios:
Chromium and Firefox each passed 9 REPL and 2 stored-body checks, with zero page
or CSP errors. The ordered lifecycle log proves each prior browser disconnected
and each prior fixture runtime exited 0 before the next scenario started. Both
body seeds followed their own runtime's joined exit and completed normally.
Results: `/tmp/whip-native-seed-diagnostics-settings-results`; full lifecycle and
progress log: `/tmp/whip-native-seed-diagnostics-settings.log`. Production renderer
remained `85b21e7e63c3226d8f683ca46cfe6e4c43ba527466a228730fb8c60f00d934d9`.

The final UTF-8 byte-accounting adjustment in stderr forwarding was additionally
validated with the unchanged actual `managed=false` fixture (19.046s). An
independent read-only review found no bound/lifetime blocker. All owned processes
joined. No hosted fix or measured performance gain is claimed.
