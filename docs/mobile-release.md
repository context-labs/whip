# Mobile build and release readiness

This is the runbook for producing and accepting Whip mobile artifacts. See
[mobile setup](mobile.md) for private-host operation and the
[evidence record](../.ai-docs/plans/mobile-app/EVIDENCE.md) for completed checks.
The initial release is iOS first, with Android following. Manual Tailscale HTTPS
connection remains the scope; app authentication, QR and notifications are deferred.

EAS has compiled simulator and signed internal iPhone artifacts using Xcode
26.6. Native simulator acceptance found and corrected an iOS SQLite path bug;
the corrected app reconnects and restores an unsent draft after force-quit.
The original artifacts contain the old JavaScript and must not be distributed.
The replacement signed phone build is ready and its signature/provisioning were
verified. The owner reports the app running on the selected iPhone. The private
`gpu-4090-sam` host now passes HTTPS/WSS and mobile-facing API reads from the Mac;
provider login, physical-device workflow acceptance and TestFlight remain separate
gates in the evidence log. See the [configured host instructions](mobile.md#configured-development-host-gpu-4090-sam).

The connection-diagnostics update adds inline modal errors and Test Connection.
Its source passes 144 mobile tests, 231 SDK tests, production exports and the
repository check. Native simulator testing reached the real GPU host and its
session list. Replacement signed preview build
`b595d18a-8b5e-4804-8211-2b3b0d9c392c` finished and its signature, approved
phone profile and embedded controls were verified. The phone disconnected before
direct installation, so installation is still pending. Use the
[exact build page](https://expo.dev/accounts/inference/projects/whipcode/builds/b595d18a-8b5e-4804-8211-2b3b0d9c392c)
on the phone. The prior preview does not gain these changes automatically.
Detailed artifact and installation results are in the evidence record.

Local Xcode was upgraded to 26.6 on 2026-09-11. The current mobile UI now compiles
and launches as a Release app on the iOS 26.5 simulator, including encrypted
storage initialization. Keep normal simulator signing enabled: explicitly
disabling signing can omit the application entitlement required by SecureStore.
The current UI's signed phone installation and interactive acceptance remain
pending; see the [UI follow-up evidence](../.ai-docs/plans/mobile-ui/EVIDENCE.md).
Expo 57 requires Xcode 26.4+. EAS is linked to the owner's `@inference/whipcode` project.
Existing local Apple API credentials registered the development bundle ID and
provisioned an Apple Distribution certificate and ad hoc profile for the owner's
selected iPhone. These signing assets are managed by EAS.
The [versioned Expo compatibility table](https://docs.expo.dev/versions/v57.0.0/)
is the toolchain reference; current attempt results belong in the evidence record.

## Automated checks

[mobile.yml](../.github/workflows/mobile.yml) runs on the root CI workflow and
contributes to its required `go` aggregate gate. It has read-only repository
permissions, uses Node 24 and the root npm lockfile, and runs mobile TypeScript,
Jest, pinned Expo Doctor 1.20.4 and production Metro exports for both platforms.
It can also be dispatched manually. It does not contact a Whip host, provision
Tailscale, start an EAS build or submit a binary.

Reproduce from the repository root:

```sh
npm ci
npm run check:mobile
npm run test:mobile
npm exec --yes --package=expo-doctor@1.20.4 -- expo-doctor apps/mobile
npm run export:mobile
```

Exports prove JavaScript bundling and package resolution. They do not compile
Swift/Kotlin/C++, test SQLCipher on a device, exercise the native UI or establish
signing/distribution. Existing SDK, web, daemon and package checks remain in
[ci.yml](../.github/workflows/ci.yml); mobile CI does not replace them.

## Local native builds

Install the supported Xcode/iOS platform and CocoaPods for iOS, or Java 17+ and
the Expo-selected Android SDK/NDK for Android. Run these from `apps/mobile` after
the root checks. Expo uses the pinned workspace CLI; no global tools are installed
by these commands.

```sh
npx expo run:ios --configuration Release
```

```sh
npx expo run:android --variant release
```

Choose the intended simulator/emulator when prompted. For a physical iPhone,
add `--device` after establishing an iOS development team and registered device.
Generated `ios/` and `android/` projects are ignored. Native configuration changes
belong in `app.config.ts` or the local storage module, then regenerate deliberately.
A local Android release build is a device-test artifact; it does not establish
store signing. A local iOS simulator build needs no App Store distribution identity.
See [Expo's local development commands](https://docs.expo.dev/more/expo-cli/).

## EAS profiles and reproducibility

[eas.json](../apps/mobile/eas.json) pins EAS CLI 23.2.0 and Node 24.14.1. Always run
EAS from `apps/mobile`. The app's `eas-build-post-install` hook builds `@whip/sdk`
before Metro uses its generated exports; a clean cloud checkout has no local
`packages/sdk/dist`. Preserve the root lockfile and this workspace build step.
See [monorepo setup](https://docs.expo.dev/build-reference/build-with-monorepos/)
and [build lifecycle hooks](https://docs.expo.dev/build-reference/npm-hooks/).

The project is [@inference/whipcode](https://expo.dev/accounts/inference/projects/whipcode),
UUID `fa9874ce-4324-474f-86ef-a8749cf8fa91`. The Expo slug follows the project;
the installed app name remains Whip. The root [.easignore](../.easignore) preserves
the Git exclusions and also excludes local research/acceptance records and
credential files from cloud build uploads. Keep its shared exclusions in sync
with `.gitignore`. Inspect an archive after changing workspace or ignore rules:

```sh
npx --yes eas-cli@23.2.0 build:inspect --platform ios --profile preview-simulator --stage archive --output ../../.ai-docs/plans/mobile-app/artifacts/eas-archive-review
```

Run this from `apps/mobile` and choose an unused output directory. Confirm the
root lockfile, all workspace sources and local native module are included, while
generated native projects, nested native-module build output, credentials and
local test data are excluded. EAS runs
Prebuild on the uploaded source; it does not use the local ignored Xcode project.
See [EAS upload exclusions](https://docs.expo.dev/build-reference/easignore/).

| Profile | Artifact/use | EAS environment |
| --- | --- | --- |
| `development` | Device development client; requires Metro | `development` |
| `simulator` | iOS simulator development client | `development` |
| `preview` | Embedded bundle; iOS internal distribution or Android APK | `preview` |
| `preview-simulator` | Embedded bundle for iOS simulator acceptance | `preview` |
| `production` | Store-signed iOS/TestFlight or Android AAB; incremented build number | `production` |

`base` only holds shared build settings. The preview profile is not a TestFlight
artifact: TestFlight uses `production`. Profile behavior follows
[Expo's build-profile reference](https://docs.expo.dev/build/eas-json/).
EAS chooses a native build image from the Expo SDK. Record the resolved image,
Xcode/JDK/SDK versions and artifact checksum with each accepted build; Node and
CLI pins alone do not make remote native output bit-for-bit reproducible.

## Owner-supplied release configuration

Complete these values before creating a distributable build. Empty fields and
development defaults are not approved production metadata.

| Required decision/value | Where it belongs | Current state |
| --- | --- | --- |
| Expo account or organization and EAS project UUID | Approved account/project; `owner` and `extra.eas.projectId` in dynamic Expo config | Linked and verified: `@inference/whipcode`, `fa9874ce-4324-474f-86ef-a8749cf8fa91` |
| Production iOS bundle ID / Android application ID | `WHIP_MOBILE_BUNDLE_ID` for the selected EAS environment and local config resolution | Owner to supply; `dev.contextlabs.whip.mobile` is the development default |
| Apple Developer team, App Store Connect application ID and signing access | Approved Apple account; EAS credential management and submission profile | Existing Apple API credentials provisioned the development bundle ID, distribution certificate and ad hoc profile; App Store Connect application record remains |
| Internal iOS preview devices | Registered device IDs / matching ad hoc provisioning | Owner selected `iPhone (40)` (iPhone 16 Pro); included in the active ad hoc profile |
| Android Play app, upload key and submission access | Approved Play account and signing configuration | Follow-on release work |
| App name/version, icons/splash and tablet support | Expo config and reviewed native assets/screens | Whip SVG/icon/splash assets are implemented; final branding review and iPad acceptance remain |
| Support/privacy URLs, contact, beta description, screenshots and review access | App Store Connect / later Play metadata | Owner to supply and approve |
| Encryption export answers and any required documentation | Store record and approved Expo `ios.config.usesNonExemptEncryption` value | Unanswered; no declaration is generated |
| Privacy/data-safety answers, third-party SDK disclosures and required-reason API manifest review | Final archive and store questionnaires | Unanswered; inspect the shipped build |

Set the bundle ID as a plain-text EAS environment variable, available during both
CLI config evaluation and remote builds. A local shell variable alone does not
configure a cloud job. Keep the local and selected EAS environment values equal;
review `npx expo config --type public` before provisioning. Follow
[EAS environment configuration](https://docs.expo.dev/eas/environment-variables/).
The dynamic config contains the verified owner/project fields. Preserve that
association when changing build profiles or native identifiers.

The encryption review must account for bundled SQLCipher as well as HTTPS and
SecureStore. Do not set the export answer to false merely because network traffic
uses TLS. The owner must determine the answer for the final binary using
[Apple's export guidance](https://developer.apple.com/help/app-store-connect/manage-app-information/overview-of-export-compliance/).
Likewise, local storage and direct private-host traffic do not by themselves
answer the store's collection/disclosure questions. Review the app and included
SDKs against [Apple's privacy definitions](https://developer.apple.com/app-store/app-privacy-details/).
No analytics, push registration or additional product login is required here.

## Owner-triggered preview and TestFlight

These commands create remote jobs or submit artifacts. Run them only after the
account, project, bundle ID and applicable signing fields above are configured.
They are documented for release operation; CI does not execute them.

From `apps/mobile`, inspect the signed-in account and resolved config first:

```sh
npx --yes eas-cli@23.2.0 whoami
npx --yes eas-cli@23.2.0 project:info
npx expo config --type public
```

Build the selected preview artifact explicitly:

```sh
npx --yes eas-cli@23.2.0 build --platform ios --profile preview-simulator
npx --yes eas-cli@23.2.0 build --platform ios --profile preview
```

For Android device acceptance, use:

```sh
npx --yes eas-cli@23.2.0 build --platform android --profile preview
```

After physical iOS acceptance, completed metadata and owner release approval,
build the store artifact and submit that exact build ID:

```sh
npx --yes eas-cli@23.2.0 build --platform ios --profile production
npx --yes eas-cli@23.2.0 submit --platform ios --profile production --id "${WHIP_IOS_BUILD_ID:?Set the reviewed production EAS build ID}"
```

Populate the real `submit.production.ios.ascAppId` and appropriate submission
credentials before this step. Inspect the chosen build's project, platform,
profile, bundle ID and version; do not rely on whichever build happens to be
latest. Production uses EAS remote build numbering; reconcile any existing store
version history before the first build. See
[app version management](https://docs.expo.dev/build-reference/app-versions/) and
[iOS submission](https://docs.expo.dev/submit/ios/).
Submission starts store processing. Record TestFlight processing, beta access and
an actual installation separately from the upload result. Public store release
remains a separate owner decision; private-host reviewer access must not expose
the owner's unauthenticated runtime publicly.

## First internal iPhone installation

Register the intended phone in the preview provisioning profile before building.
Internal iOS builds require Developer Mode on iOS 16+. Connect the unlocked phone
to the Mac, accept the phone's Trust prompt if shown, and select it in Xcode's
Window → Devices and Simulators. On the phone, open Settings → Privacy & Security
→ Developer Mode. Enable it, restart, unlock, and confirm Turn On.

If the setting is absent, install the accepted preview from its EAS build page
on the phone and tap the Whip icon once. The Developer Mode alert can expose the
setting. The owner performs the phone-side confirmation and passcode entry.
Use only the exact accepted build link: an older successful build may contain
superseded code. See [Expo's Developer Mode guide](https://docs.expo.dev/guides/ios-developer-mode/).

## Acceptance record before beta distribution

Record the commit/lockfile, EAS build ID or local command, toolchain, artifact hash,
device/OS, fixture and outcome in
[EVIDENCE.md](../.ai-docs/plans/mobile-app/EVIDENCE.md). Leave failures and untested
items explicit. Reuse the phased plan's detailed gates; this is the release subset.

- [x] Supported-toolchain iOS native compile and launch; release configuration
  embeds the bundle and runs without Metro.
- [ ] Physical iPhone Wi-Fi/cellular access to the intended private host, TLS
  failure behavior, VPN loss/return, host sleep/restart and runtime replacement.
- [ ] Phone/web parity for streaming, send/steer/stop, root/child selection, new
  session partial failure, batched questions and once/deny decisions.
- [ ] Process death around admission, foreground reconciliation, exact-ID recovery
  and preserved edited drafts without automatic resubmission.
- [ ] Real SecureStore/SQLCipher key-loss, backup exclusion, upgrade and quota
  behavior; mocked storage tests alone do not establish these properties.
- [ ] Keyboard, native selection, sheets, rotation, VoiceOver, large text,
  light/dark themes, long transcripts and repeated navigation on named devices.
- [ ] Reviewed native assets, supported-device scope, archive privacy manifests,
  permission strings and owner-approved store/export metadata.
- [ ] Correct signed artifact accepted by TestFlight and installed by an intended
  tester, with usable setup/support instructions.
- [ ] Android native/device/store acceptance recorded before Android distribution;
  successful Android bundle export does not satisfy this follow-on gate.

EAS account access belongs to build/distribution administration. It does not add
login to the mobile product. There are no QR, notification, public-relay or app-auth
release requirements in this checklist.
