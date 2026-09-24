# Native editor and IPC acceptance

The native capability is implemented and its current protocol 5/schema 12 IPC
checks pass. Actual installed Cursor, VS Code, and Zed opened the full local test
directory containing spaces, `#`, `%`, and Unicode. Cursor and VS Code also
accepted the exact remote directory and authenticated against an isolated SSH
server. Remote server installation and folder browsing were not validated.

## Recorded results

- [Installed editor acceptance](native-editor-acceptance.json) records the
  observed local launches and Cursor/VS Code SSH handoffs. Cursor used its
  AnySphere Remote SSH extension; this result was not inferred from VS Code.
  The temporary SSH server deliberately refused server setup after authentication.
- [Current IPC acceptance](native-ipc-smoke.json) uses renderer
  `be7376fa02f71e74262ef2ffc167a7dde4a9b4d723d15bbc0f1fab065affa75d`,
  protocol 5 and schema 12. It runs the actual sandboxed preload and native main
  against isolated daemons. It verifies fixed application discovery, stale runtime
  rejection, missing-directory errors, remote sources never becoming local folders,
  required explicit aliases, URL-source disposal cancelling an in-flight identity
  probe, and detached native source rejection. It launches no external application.
- [Earlier IPC acceptance](native-ipc-smoke-protocol4.json) additionally confirmed
  an actual Finder launch, using the earlier protocol 4/schema 11 build. It is
  retained separately and is not evidence for the final wire version.
- Native tests: 80 total, 71 passed, 9 existing optional SSH helper integration
  tests skipped. The 8 project-opening tests passed. Desktop adapter tests: 10
  passed. Desktop and shared-app type checks passed.

These are staged production-asset/native IPC checks. They do not establish
signed installed-app acceptance or publish a release.

## Remaining editor checks

Zed 1.18.1 opened the local test folder. Its macOS singleton prevented running a
separate instance with disposable remote settings while the user's existing Zed
windows stayed open. Zed remote authentication and browsing remain unverified;
the user's existing windows were preserved. The implementation uses Zed's
[documented SSH URI format](https://zed.dev/docs/remote-development), and argument
tests verify literal path encoding.

Launch success means the installed editor accepted the folder request. Editor
onboarding, SSH host trust, credentials, remote extensions, and remote server
setup can still need attention inside that editor. Some fresh Cursor test profiles
stalled before starting SSH; successful runs are recorded separately above.

## Keychain incident and fixture cleanup

The first editor harness supplied a synthetic `HOME` without a normal macOS login
keychain. It also sent SIGTERM to detached editor GUIs and deleted fixture data
after a fixed 500 ms without confirming process exit. Cursor outlived that cleanup
and displayed missing-keychain prompts for `Cursor Key`. The coordinating task
stopped the orphan fixture process (PID 58359); the user's normal Cursor process
(PID 55077) was left running. Live editor testing then stopped.

The harness now requires explicit opt-in, preserves the real OS `HOME`, isolates
editor user data with `--user-data-dir`, and waits for its detached GUI processes
to exit before removing data. If necessary, it force-stops only processes still
advertising its unique fixture directory; it preserves the directory if any remain.
Fixture-only `--password-store=basic` and `--use-mock-keychain` switches were added.
Both installed Electron Framework binaries contain `use-mock-keychain`, but its
effect on Cursor's custom credential module has **not** been verified. No editor
launch was rerun after this change. Production editor launches use none of these
fixture switches.

[Cleanup record](native-fixture-cleanup.json) lists the temporary directories
removed after checking process arguments and open-file references. Disposable SSH
keys and configuration were removed with those directories. Real editor homes,
SSH configuration, keychains, and the user's Whip daemon were not edited by this
cleanup. The test-created Zed window was closed individually; existing windows
were preserved.

## Reproduction

The current nonlaunch IPC diagnostic is:

```sh
node apps/desktop/scripts/project-editors-ipc-smoke.mjs
```

It requires a previously staged desktop and a current packed renderer. It copies
the staged assets, rebuilds native main/preload and the backend into a temporary
directory, and defaults to **no external application launch**. An explicit
`WHIP_EDITOR_IPC_OPEN_FINDER=1` adds the Finder launch check.

The manual installed-editor diagnostic is intentionally opt-in and should not be
run unattended until the macOS credential behavior is verified:

```sh
WHIP_EDITOR_SMOKE_ALLOW_APPS=1 node apps/desktop/scripts/project-editors-smoke.mjs
```

`WHIP_EDITOR_SMOKE_APPS=cursor,vscode,zed` selects targets. The harness chooses an
already installed Remote SSH extension compatible with the installed editor; it
does not install extensions into the user's profile. Zed is skipped while it has
existing windows. `WHIP_EDITOR_SMOKE_KEEP=1` retains diagnostic data after owned
processes have been stopped. No real remote host is contacted.
