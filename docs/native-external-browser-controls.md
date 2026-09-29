# External Chrome from the CLI and terminal

External Chrome is separate from Desktop's offered tabs. These human controls
configure the native host and manage its named connection metadata. They do not
grant an agent access or open a browser as a side effect.

Start with `whipcode browser status` (or `status --json`). It reports the complete
configuration, host revision, effective driver and whether the driver is pinned.
`whipcode browser configure --revision HASH --mode MODE` replaces the complete
declaration using that exact revision. Its optional flags are `--executable`,
`--live-endpoint`, `--live-profile` and `--allow-private-urls`. Omitted source
fields are cleared, and private URLs default to denied.

The supported modes are `disabled`, `live`, `dedicated`, `headless` and
`extension`. Dedicated/headless require an absolute executable on the host.
Live requires exactly one literal-loopback HTTP/WebSocket endpoint or absolute
host profile. Extension assets remain an explicit `whipcode browser install`
action; a later authorized agent operation owns relay startup.

`whipcode browser list [--json] SESSION` reads up to four root-owned named
connections. A child can inspect this metadata. Root controls take the exact
displayed identity:

```text
whipcode browser reconnect ROOT NAME GENERATION
whipcode browser disconnect ROOT NAME GENERATION
```

Reconnect requires an active root and prepares a fresh generation. It does not
open Chrome, publish relay credentials or replay earlier work. Disconnect retires
only the captured generation. Neither action accepts a child ID as the root.

In the terminal, `/browser external status` supplies a copyable full declaration
and explains every field. `/browser external list` reads connection metadata.
The selected root can use `/browser external configure REVISION {complete JSON}`
or `/browser external reconnect|disconnect NAME GENERATION`. Configuration is
bounded to 32 KiB and rejects omitted fields and unknown properties. A selected
child has only status/list access, including read-only Browser driver status.

No control silently refreshes a revision or resends an uncertain mutation.
After a lost acknowledgement, read status/list and explicitly choose any further
action. The terminal's `/retry` does not replay these browser controls. Agent
permission checks remain separate and use the new exact resource.

`cmd/whip/browser_native_test.go` exercises real native configuration CAS,
generation replacement, stale-generation rejection, accepted-but-lost replies
and passive recovery without browser/provider startup. The terminal's
`native_external_browser_test.go` covers complete declarations, real child
read-only behavior and all three lost-acknowledgement controls. These are client
control tests; actual Chrome/driver tests remain the separate native browser gate.
