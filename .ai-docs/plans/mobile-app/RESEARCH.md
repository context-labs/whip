# Mobile companion research record

Date: September 7, 2026. Scope: repository inspection, official documentation,
maintainer repositories, and read-only npm registry queries. No native prototype,
network exposure, device testing, package installation or deployment was performed.

The [architecture proposal](README.md) contains the proposed design and the
[phased implementation plan](IMPLEMENTATION.md) maps it to the current web app. This record
separates observed capabilities from recommendations and experiments still needed.
Current user direction: iOS first, Android shortly afterward, manual server URL
entry on the same Tailscale network, and no app authentication or notifications
for now. This supersedes the original QR/push scope. Earlier push findings below
are retained only as future research, not release requirements.

## Version and tooling snapshot

Read-only registry queries were run with `npm view`:

| Query | Observed result | Meaning |
| --- | --- | --- |
| `expo dist-tags --json` | latest/next `57.0.20`; canary `58.0.0-canary-20260902-26df09e` | Start from stable SDK 57, not canary |
| `react-native@0.86.3 peerDependencies engines --json` | React `^19.2.3`; Node accepts `^24.3.0` | Existing React 19.2.8/Node 24 can fit the peer range; still run Expo validation |
| `@expo/ui version --json` | `57.0.16` | SDK-aligned library available for the component spike |
| `react-native-paper version peerDependencies --json` | `5.15.3`; RN/React/safe-area peers | Available fallback; loose peers are not a compatibility guarantee |
| `@shopify/flash-list version peerDependencies --json` | `2.3.2` | Current stable package snapshot; use SDK-compatible version |
| `react-native-enriched-markdown version peerDependencies --json` | `1.0.2` | Evaluate this version, not an old blog tutorial's pin |

Registry references: [Expo](https://registry.npmjs.org/expo/latest),
[React Native 0.86.3](https://registry.npmjs.org/react-native/0.86.3),
[Expo UI](https://registry.npmjs.org/@expo%2fui/latest),
[Paper](https://registry.npmjs.org/react-native-paper/latest),
[FlashList](https://registry.npmjs.org/@shopify%2fflash-list/latest),
[Enriched Markdown](https://registry.npmjs.org/react-native-enriched-markdown/latest).
These URLs move; the values above are the dated command results.

The [Expo SDK 57 release notes](https://expo.dev/changelog/sdk-57) identify RN 0.86
and React 19.2, with important fixes in later patches. The prior
[SDK 56 notes](https://expo.dev/changelog/sdk-56) explain Expo UI's native APIs
becoming stable and the newer native build requirements. Recommendation: use
Expo's compatible dependency set, pin it, and retain a versioned device test record.

### Why Expo

| Finding | Consequence for Whip |
| --- | --- |
| Development builds support native libraries/configuration beyond Expo Go | Use `expo-dev-client` immediately; native controls, SQLCipher and any necessary network adapter are tested in Whip's own binary |
| Expo supports npm monorepos and shared packages | Add `apps/mobile` in the existing workspace; inspect duplicate React/native dependencies rather than preemptively replacing Metro config |
| Router supplies file-based native navigation/deep links | Use it for native screens; keep the existing web router in the web application |
| EAS Build/Submit automate artifacts and submission | Useful release tooling without a new app backend; store access/review remains separate |

Sources: [development builds](https://docs.expo.dev/develop/development-builds/introduction/),
[monorepos](https://docs.expo.dev/guides/monorepos/),
[Router](https://docs.expo.dev/router/introduction/),
[EAS Build](https://docs.expo.dev/build/introduction/),
[submission](https://docs.expo.dev/deploy/submit-to-app-stores/).

Plain React Native CLI would remain possible, but supplies no specific advantage
for this scope over Expo development builds. A PWA would reuse more web rendering,
but would not fulfill the requested React Native direction. A WebView wrapper
would also leave the core reading/input experience tied to browser behavior.
Those are architecture judgments, not benchmark findings.

## Component libraries

### Expo UI: primary candidate

[Expo UI](https://docs.expo.dev/versions/latest/sdk/ui/) exposes native SwiftUI and
Jetpack Compose controls. Its [universal layer](https://docs.expo.dev/versions/latest/sdk/ui/universal/)
offers common controls through a single API and uses a Host boundary. This matches
Whip's limited need for controls, settings and sheets while RN handles chat layout.

It does not promise identical pixels across platforms or mean that RN children
can be nested arbitrarily inside a native subtree. Validate theme mapping,
RNHostView placement, text input focus, accessibility and list/sheet interaction.
The recommendation favors native interaction and a small maintained dependency
set, not maximum cross-platform visual sameness.

### Paper: fallback

[React Native Paper's maintainer repository](https://github.com/callstack/react-native-paper)
describes a Material Design component library for iOS/Android. Its
[theme integration guide](https://callstack.github.io/react-native-paper/docs/guides/theming-with-react-navigation)
documents the Material 3 color model. The package was available as 5.15.3 during
research. Recommendation: prefer it if Expo UI bridges/platform exceptions dominate
the spike; accept more Whip styling work and a Material base on iOS.

### Tamagui

The [Tamagui compiler documentation](https://tamagui.dev/docs/intro/compiler-install)
states that the compiler is optional; it is incorrect to reject Tamagui because
it inherently requires a compiler. It offers a broad styling/component approach
and native/web reuse. Here the existing web implementation is not being replaced,
so much of that reuse benefit would be unrealized. That makes it a less economical
choice for this particular companion, not a judgment that it is generally slow
or unsuitable.

### React Native Reusables

The [maintainer repository](https://github.com/founded-labs/react-native-reusables)
provides open-source components influenced by shadcn/ui and integrates with
NativeWind/Uniwind. It could reproduce Whip's restrained custom style. The tradeoff
is owning copied component source and another styling toolchain. Avoid interpreting
GitHub stars or anecdotal library complaints as production evidence.

### gluestack UI

The current [introduction](https://gluestack.io/ui/docs/home/overview/introduction)
describes component source that is copied/customized and integrates with
Tailwind/NativeWind. Its broad catalog is useful, but Whip needs only a subset and
does not need a new universal design-system migration. It is a viable alternative
without a demonstrated benefit over the chosen primary/fallback pair.

### Bounded UI experiment

Use the same fixture for the candidate implementation:

1. Session list with 200 loaded rows, directory grouping and selection.
2. Conversation with mixed messages, streamed text, long code/table and native copy.
3. Composer with multiline growth, keyboard show/hide, dictation/paste and rotation.
4. A three-page question with single-select, multi-select, skipped response and
   free text; a permission sheet with a long path and arguments.
5. Theme switch, large text, VoiceOver/TalkBack and modal focus restoration.

Record responsiveness, native/JS memory, anchor stability, implementation size,
and platform exceptions. Default to Expo UI. Switch to Paper only on a concrete
failure that would cost more to work around than changing libraries. Do not
implement the same full app five times or benchmark unrelated showcase demos.

### Reading and keyboard libraries

[FlashList usage](https://shopify.github.io/flash-list/docs/usage/) documents
position-maintenance options useful for chat. [Known issues](https://shopify.github.io/flash-list/docs/known-issues/)
show that position maintenance can affect reordered data. Stable keys, following,
pagination and selection therefore need explicit testing; list virtualization
does not bound retained data.

[Enriched Markdown](https://github.com/software-mansion/enriched-markdown) is a
native Markdown renderer with selection and streaming features. A maintainer
[issue about full-document reparsing](https://github.com/software-mansion/enriched-markdown/issues/391)
is a reason to measure long streaming messages, not proof that the current release
has that same performance problem. Test blocked external images/links and large
content before adoption; fall back to bounded plain/native text while investigating
any rendering failure rather than hiding a fetch or executing HTML.

[Expo's keyboard-controller reference](https://docs.expo.dev/versions/latest/sdk/keyboard-controller/)
is the integration starting point. Keep Expo-aligned Reanimated/worklet versions
where required, and test the actual compositor/keyboard path on devices.

## Network and security findings

| Observed fact | Design implication |
| --- | --- |
| Tailscale Serve shares a local service within a tailnet; Funnel is public | Use Serve to proxy the existing trusted loopback listener; tailnet access rules are the deliberate beta access boundary. No public mapping |
| Serve can terminate HTTPS and proxy to localhost | Keep standard TLS validation in the phone and reuse the existing listener. Loopback source behind a proxy does not identify an individual phone |
| Tailnet certificate issuance uses public certificate transparency | Host/tailnet names are metadata exposure; use a neutral hostname |
| Cloudflare Tunnel uses outbound connectors | Future option only; public reachability needs a separate app authentication/access design and accepts provider TLS termination |
| Native networking does not enforce browser CORS | Origin filtering cannot authenticate a phone or constrain a malicious native client |
| RN WebSocket source accepts a third options argument with headers | Future auth integration is feasible in principle; no header-auth adapter is required for the current scope |
| RN documents limitations in fetch redirects/cookies | Validate actual native bounded-content behavior and prevent silently switching the configured host; no server credentials are sent in this release |

Sources: [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve),
[Tailscale HTTPS](https://tailscale.com/docs/how-to/set-up-https-certificates),
[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/),
[React Native networking](https://reactnative.dev/docs/network),
[RN 0.86.3 WebSocket source](https://github.com/react/react-native/blob/v0.86.3/packages/react-native/Libraries/WebSocket/WebSocket.js).

The [OWASP WebSocket checklist](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html)
is a reference for future application-authentication work and current transport,
resource-bound and logging checks. The revised beta deliberately trusts every
client allowed to reach the endpoint through Tailscale; it does not implement
per-phone authentication, roles or revocation. Preserve the existing Host/Origin,
content-scope and payload checks, without treating them as phone identity.

Manual URL entry replaces QR onboarding. A stored runtime ID only protects local
state continuity; it is not cryptographic authentication of the server. HTTPS
validates the endpoint and Tailscale policy controls reachability. These are
explicit design choices for the private beta, not a claim that a standard endorses
unauthenticated public access.

Direct LAN support has an additional platform permission burden:
[Apple local-network privacy](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy)
and [Android target-dependent local-network access](https://developer.android.com/privacy-and-security/local-network-permission).
Do not assume every Tailscale route has the same permission classification as a
LAN address. Test both denied access and VPN routes on actual target OS builds.

## Storage and native portability

[SecureStore](https://docs.expo.dev/versions/latest/sdk/securestore/) is for small
secrets, with platform-specific persistence, backup and accessibility behavior.
Database-sized values do not belong there. This release stores only the local
database encryption key in SecureStore, with no daemon grant or login credential.
Treat missing keys, backup restore and surviving iOS Keychain entries on reinstall
as explicit local-data lifecycle events.

[Expo SQLite](https://docs.expo.dev/versions/latest/sdk/sqlite/) offers a SQLCipher
build option; it is not enabled by default. Use it in Whip development/release
builds and keep the database key in SecureStore. This is less application code
than inventing record encryption. [Expo Crypto](https://docs.expo.dev/versions/v57.0.0/sdk/crypto/)
provides randomness/digest facilities for SDK adapters where native globals differ.

The current SDK assumes several browser/Node-style globals and stream methods.
The repo plan names every identified dependency. Documentation and TypeScript
compilation cannot prove those work in Hermes or that cancellation/byte bounds
survive an adapter. Keep the device spike as an explicit gate.

## Notifications (deferred research)

Notifications are out of the initial release. Do not add `expo-notifications`,
request OS notification permissions, create a daemon outbox or operate a gateway.
The earlier managed-versus-self-hosted question is no longer pending. Keep the
foreground Attention queue and refresh session status when returning to the app.

For a future milestone, [BackgroundTask](https://docs.expo.dev/versions/latest/sdk/background-task/)
is OS-scheduled work, not a continuously connected real-time client. Background
polling is not a substitute for timely question/completion push alerts.

The [Expo push FAQ](https://docs.expo.dev/push-notifications/faq/) explains that
native APNs/FCM delivery is also possible without Expo Push. Expo Push offers
best-effort delivery to platform providers; receipts do not prove a person saw
the notification. An authoritative in-app queue remains necessary.

[Expo enhanced push security](https://docs.expo.dev/push-notifications/sending-notifications/#additional-security)
can require an access token. A future gateway could hold a shared app project's
secret rather than distributing it to every daemon. Its enrollment, token
ownership, quotas, revocation and privacy would still need design. Direct
APNs/FCM or an operator-owned app/gateway remain alternatives. No service model
is selected or required by the revised plan.

## Distribution and validation

[EAS Update runtime versions](https://docs.expo.dev/eas-update/runtime-versions/)
separate native-binary compatibility from JavaScript updates.
[Update signing](https://docs.expo.dev/eas-update/code-signing/) is currently a
paid-plan capability in managed EAS. Recommendation: store-only releases first;
add controlled signed OTA only when its benefit justifies its operational cost.

Current service costs should be selected from the actual account's needs and
[Expo pricing](https://expo.dev/pricing); this research did not purchase or select
a subscription. A public app also needs store accounts, metadata and reviewer
access. Read [Apple review requirements](https://developer.apple.com/app-store/review/guidelines/)
against the final build and provide a safe demonstration path for a private-host
companion. No plan can guarantee app-store approval.

[Expo Jest/RNTL guidance](https://docs.expo.dev/develop/unit-testing/) and
[Maestro on EAS Workflows](https://docs.expo.dev/eas/workflows/examples/e2e-tests/)
cover repeatable app tests. Physical network transitions, accessibility, keyboard
behavior and native selection remain separate evidence requirements. Camera and
notification testing are deferred with those features.

## Research limitations

- Docs describe supported APIs; no selected combination has been run in this repo.
- Some library websites were inaccessible to the browser tool; their own
  repositories or other maintained documentation supplied the evidence cited here.
- Search results sometimes returned older documentation. Stable release notes,
  current package metadata and current source took precedence.
- Existing web documentation and implementation are evolving in the working tree.
  Wire major 4.1 was verified in source despite legacy `/api/v3` endpoint names.
- Effort estimates in the plan are engineering judgments. Native UI composition
  and SDK/network portability are the largest uncertainties in the reduced scope;
  no runtime performance numbers are claimed as measured.
