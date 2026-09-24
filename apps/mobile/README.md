# Whip mobile

Expo/React Native companion for existing Whip hosts, using manual Tailscale HTTPS
URLs. See [setup and development](../../docs/mobile.md), the canonical
[frontend guide](../../docs/frontend.md), and
[implementation evidence](../../.ai-docs/plans/mobile-app/EVIDENCE.md).

Run workspace commands from the repository root:

```sh
npm run check:mobile
npm run test:mobile
npm run export:mobile
npm run dev:mobile
```

A native development build is required. Expo Go cannot provide the encrypted
storage/native UI modules. Native projects are generated from `app.config.ts`;
`modules/whip-storage` contains the small backup-exclusion bridge.

## Manual acceptance fixture

Use the existing SDK fake-provider daemon for local phone/web comparison. From
the repository root, build and pack the web app before compiling the fixture:

```sh
npm run pack:web
node apps/mobile/scripts/fixture.mjs --minutes=30
```

The runner prints a loopback server URL, temporary host directory, existing root
and a web conversation URL. Enter the server URL in a **development** mobile build
and open the web URL on the host to observe the same session. The packaged web app
is embedded when the fixture compiles; rebuild/restart the fixture to pick up web
changes. Production mobile builds require HTTPS and cannot use this HTTP fixture.

An iOS simulator reaches the printed host loopback address directly. For a USB
Android device or emulator, explicitly forward its loopback port in a second
terminal, replacing `PORT` with the printed server port and `SERIAL` with the
intended `adb devices` entry:

```sh
adb -s SERIAL reverse tcp:PORT tcp:PORT
```

Enter the same `http://127.0.0.1:PORT` URL on Android. Remove that specific mapping
afterwards with `adb -s SERIAL reverse --remove tcp:PORT`. The runner never sets
up forwarding, changes Tailscale or touches the normal Whip daemon. A physical
iPhone cannot use the computer's loopback URL; private-network/cellular acceptance
is a separate setup described in [mobile setup](../../docs/mobile.md).

Ordinary text is echoed with stream events. `hold:manual` starts cancellable held
work, and `permission:manual` requests a test write inside the temporary workspace.
Use Stop or Allow once/Deny to inspect those workflows. `question:single` requests
one choice. `question:batch` requests a single choice, multiple choices/custom text,
and an optional page to skip; completion echoes the answers as JSON. Answer on
either client and verify the other removes the pending request. The fixture does
not contact a real model. Type only test content; accepted text and effects are
recorded inside the temporary directory.

Ctrl-C/SIGTERM cleans up the fixture. It also stops just before its bounded
deadline; `--minutes` accepts 1–30 and defaults to 30. SDK acceptance callers retain
their four-minute default. Temporary data is deleted unless the existing explicit
`WHIP_SDK_KEEP_FIXTURE` diagnostic setting is enabled.
