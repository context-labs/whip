# Runtime race gate timeout and fixture cleanup

The macOS `race-runtime-first` job on draft #276 timed out after 600.247s
at shuffle seed `1790689853529386000`. The group selected `^Test[A-M]`.
At the deadline, `TestBrowserTransferUnknownDeliveryRetiresBothAndNeverSpawns`
had run for only one second. Its stack was runnable inside SQLite schema
initialization through `Store.Open`. This is evidence of aggregate deadline
exhaustion, not a demonstrated hang in that test. Compilation occurred before
the test process's ten-minute timer. A later integrated local run of the same
group passed in 301.413s; this does not establish a hosted timing guarantee.

The dump separately contained eleven SDK `streamableServerConn.Read` goroutines
from earlier cases, some idle for five minutes. The runtime MCP HTTP fixture
owned an `httptest.Server` and an SDK server, but only closed the HTTP listener.
The SDK retains stateful sessions independently of HTTP request lifetime and
defaults to no idle timeout. Client cancellation can prevent the final HTTP
DELETE from reaching this owned server.

`TestMCPHTTPFixtureJoinsRetainedServerSessions` reproduced the orphaned session
after a nested runtime fixture exited (red in 1.076s). Teardown now closes and
joins HTTP connections, then closes all remaining fixture-owned SDK sessions.
All `TestMCP` runtime tests passed race/shuffle in 14.012s. Production cancellation,
remote server policy and MCP session semantics are unchanged. These idle sessions
were a concrete fixture leak; no measurement establishes that they caused the
600-second timeout.

The runtime race gate now uses three complementary name groups: `^Test[A-C]`,
`^Test[D-M]`, and the complement of `^Test[A-M]`. At this checkpoint they select
82, 70 and 82 declarations. The final group continues to include new names,
examples and fuzz seeds outside the two ranges. Every group retains coverage
output, `-p=2`, race detection, shuffle, one uncached run and `-timeout=10m`.
Both hosted operating systems require all three through the existing fail-closed
`checks` matrix and aggregate job.

Validation executed the actual `task check:race-runtime` shell orchestration with
an intercepting Go command against the real compiled test-name inventory and
nine future boundary/example/fuzz names. Every name appeared exactly once, and
the original flags and deadline were checked. Actionlint passed. This validates
selection and wiring, not the runtime duration of all three groups; the final
integrated and hosted gates remain required.
