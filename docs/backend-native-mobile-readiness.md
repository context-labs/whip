# Native mobile readiness — d90668cc3

Source: `/private/tmp/whip-native-mobile-readiness`, branch `codex/backend-redesign-mobile-readiness`, exact commit `d90668cc3aabd24a2e5c7511e951668a70a47763`. Tracked source is unchanged. Generated native projects, dependencies, build products, simulator and logs are disposable validation artifacts, not source changes.

## Toolchain and prerequisites

macOS26.3.1 arm64; Xcode26.6 build17F113; Swift6.3.3; Node24.14.1/npm11.11.0. Existing CocoaPods1.17.0 and Ruby4.0.6 selected by per-command PATH. No system configuration or tool installation changed. SQLCipher is enabled on both generated native projects. ReactNativeEnrichedMarkdown's pinned postinstall restored required grammars/RaTeX assets in this checkout before pod installation. 117 dependencies/116 pods installed.

Root package-lock SHA256: `2ba06c3e0888a15fd2df57828f754e85f0e16423b48cf85758dbf79533a00ae2`.

## Completed checks

- Mobile TypeScript: PASS (`/tmp/whip-mobile-readiness-types.log`).
- Mobile unit suites: 34 suites/219 tests PASS,25.856s (`/tmp/whip-mobile-readiness-tests.log`).
- Compiled native backend fixture:5 tests PASS,21.946s; both engines, root/child scopes, permissions/questions, traces/content, stdout/accounting, lost ACK/restart/no replay, deny/reload and driver CAS (`/tmp/whip-mobile-readiness-backend.log`).
- Manual fixture tests:2 PASS,9.421s; exact external Origin/Host checks and joined fixture CLI shutdown (`/tmp/whip-mobile-readiness-manual-fixture.log`).
- Expo Doctor1.20.4 offline:21/21 PASS (`/tmp/whip-mobile-readiness-doctor.log`).
- Production iOS and Android Hermes export:PASS (`/tmp/whip-mobile-readiness-export.log`).
- Desktop TypeScript:PASS (`/tmp/whip-mobile-readiness-desktop-types.log`).
- Desktop unit/native suite:156 passed,10 optional real SSH cases initially skipped; distribution suite116 passed, startup-probe self-test passed (`/tmp/whip-mobile-readiness-desktop-tests.log`).
- Those10 real SSH cases separately ran against a newly compiled helper and fresh loopback sshd/keys/home:10 PASS,10.130s (`/tmp/whip-mobile-readiness-ssh.log`). No saved SSH hosts/keys/config or installed runtime was used.
- Initial `swift test --package-path driver` exposed missing testTarget after a successful compile. Agent1 owns the independent minimal manifest/CI repair; this source tree was not modified (`/tmp/whip-mobile-readiness-swift-red.log`).

## Actual iOS simulator

Release arm64 simulator build PASS with normal simulator signing, two jobs, no Metro and no signing-disable flags. Command: `xcodebuild -workspace Whip.xcworkspace -scheme Whip -configuration Release -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' ARCHS=arm64 -jobs 2 -derivedDataPath /private/tmp/whip-native-mobile-readiness-build build`. Log `/tmp/whip-mobile-readiness-xcodebuild.log` ends `BUILD SUCCEEDED` and includes normal CodeSign.

Created only iPhone17Pro/iOS26.5 simulator `08C18052-6217-4967-84C0-63152C5C7731`, named `Whip native v4 readiness d90668cc3`. Installed only the owned build, launched `dev.contextlabs.whip.mobile`, observed the native “Connect a host” screen. Terminated and relaunched the same app successfully; no storage error. The initialized SQLCipher database was4096 bytes with WAL8272 bytes and SHM32768 bytes; plaintext SQLite could not read it and it had no plaintext SQLite header. Its bytes/hash were unchanged after relaunch. SecureStore key was never read or printed. This proves initialization/reopening on this simulator, not physical-device key-loss or backup restoration behavior.

Screenshots:
- `/tmp/whip-mobile-readiness-first-launch.png`
- `/tmp/whip-mobile-readiness-relaunch.png`

Storage evidence: `/tmp/whip-mobile-readiness-storage-first.json` and `/tmp/whip-mobile-readiness-storage-relaunch.json`.

Owned simulator was terminated, shut down and deleted after acceptance. `simctl list devices booted` is empty. Existing simulators/apps/devices were not modified.

Built app: `/private/tmp/whip-native-mobile-readiness-build/Build/Products/Release-iphonesimulator/Whip.app`.
- Executable33930848 bytes SHA256 `c54c0ea551841bbd9b9dcd6df9c1decef3aabfbfb463e0241ffc520b8fc5b5f2`.
- Embedded main.jsbundle10472612 bytes SHA256 `52616508ab1fb96f9ee08636c6d3ec34b69c7d6a50a436c9674ffb9f6facf0cb`.
- Exported iOS Hermes10472623 bytes SHA256 `00d077aea73fe06c0baf143455b9b5c08ec1279711995571c26592cea5ed0a4a`.
- Exported Android Hermes10580488 bytes SHA256 `ec126bfc8c06afa672581da4aa885e6b7215dc478bc475b145d0b9181e7d96a0`.

## Remaining limits

No physical phone, private Tailscale connection, real account, EAS/TestFlight/store release, distribution signing, TLS override or installed runtime was used. Release rejects HTTP loopback intentionally; simulator UI/backend workflow parity is not inferred from the separate SDK/runtime fixture. Physical Wi-Fi/cellular/VPN/host replacement, UI foreground/death recovery, key-loss/backup/upgrade/quota, keyboard/accessibility/rotation/large text and signed-device acceptance remain the runbook's explicit gates. Additive external-browser mobile UI under development on agent3's branch is not covered by this exact baseline.

## Android native compile

Local Release arm64-v8a APK build PASS in2m39s,949 tasks executed, using existing JDK17.0.20/SDK36/NDK27.1.12297006/Gradle9.3.1 and two workers. Command: `JAVA_HOME=/opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home ANDROID_HOME=/Users/samheutmaker/Library/Android/sdk ./gradlew :app:assembleRelease --no-daemon --max-workers=2 -PreactNativeArchitectures=arm64-v8a`. Log `/tmp/whip-mobile-readiness-android-build.log`.

APK: `/private/tmp/whip-native-mobile-readiness/apps/mobile/android/app/build/outputs/apk/release/app-release.apk`,69091823bytes,SHA256 `c341704793361a572bd30e8661b602f770c2dc6a98234f999651d8cfd8ddb9b9`. Signature verified with existing apksigner; generated native Release currently uses the disposable debug signing key, so this is explicitly a local test artifact, not store signing. Signature evidence `/tmp/whip-mobile-readiness-android-signature.log`.

No adb/emulator/device operation was run. Android device behavior, actual SQLCipher initialization on Android and distribution signing remain untested. All build/test commands joined and tracked source remains clean.
