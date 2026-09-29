# Native computer helper retirement

The native controller already uses `OpenConnection`, explicit helper publication
and generation-bound operation authority. The old `Helper`, process singleton,
ambient `WHIP_COMPUTER_BIN` override, implicit extraction to the user directory,
development-tree fallback and crash-restart/retry path have no production
callers. This increment removes them instead of retaining a second transport.
The embedded payload, explicit bundle publisher, wire values, image decoder,
native connection/controller and AppleScript leaves remain.

Nine old helper tests and two old mixed-file transport tests are retired with
their private fake process. The pure screenshot decoding test remains unchanged.
The old ambient distribution-path test is superseded by explicit publication
tests, which verify exact bytes, private owned directories, symlink rejection
and passive availability. Native connection tests retain exact token/handshake,
scoped environment, stale-code mapping, malformed/oversized frames, cancellation,
bounded waiters and joined lifetime. The missing-announcement, rejected-handshake
and mismatched-handshake cases are additionally ported to `OpenConnection`.

Automatic restart/replay and process-global error caching are deliberately
removed semantics. `TestOwnedHelperTransportLossNeverReplaysOrRestarts` records
one actual effect, refuses new calls on the ended generation and requires an
explicit new connection. Controller tests cover the matching human reconnect,
policy and permission boundaries. The removed unsupported-platform extraction
assertion is obsolete because installation reads only an embedded payload;
empty/non-macOS builds report `ErrBundledUnavailable` without searching paths.

The Swift driver documentation now describes native ownership and setup rather
than the removed automatic installer. No installed helper, user application or
macOS permission setting is changed by this cleanup.

Validation: the complete remaining computer race/shuffle suite passes in6.513s,
the execution import-boundary check passes in0.645s, native runtime/CLI test
packages compile, and computer vet and frozen-baseline pinned lint pass. The
first lint run found an unused old request DTO; removing that final unused type
produced zero issues. Actual macOS permission grants are separate platform
acceptance and are not claimed by these process fixtures.
