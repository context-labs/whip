# Whip startup splash

Add the centered `WhipcodeWordmark` to the shared browser/desktop renderer at
startup. Reuse HALO's animation from `halo/app/src/routes/__root.tsx`
(`StartupTransition`) and `halo/app/src/mainview/styles.css`: 220ms fades,
420ms logo rise with an 80ms delay, and a 220ms content entrance with a 260ms
cleanup window. Use Whip theme tokens and the existing SVG component.

`apps/web/src/bootstrap.tsx` passes its initial host-connection promise through
`packages/app/src/index.tsx` to `startup-screen.tsx`. The application stays
mounted but inert while covered. No minimum hold; release after the Local connection
attempt settles or three seconds so an offline host cannot trap users. Remote
connections continue in the background. Desktop
prompts stay outside the inert subtree, and native window readiness is unchanged.
Navigation and reconnects do not replay startup. Saved and system reduced-motion
settings disable animations.

Validate StrictMode startup, failure, timeout, reduced motion, disposal and
navigation with focused tests; compile extracted StyleX in the production build;
inspect the centered logo, transitions and narrow/light/dark layouts in Chromium.

Validation completed: 14 tests across startup-screen, bootstrap and architecture;
`npm run check:web` (typecheck and production build); Chromium checks at 1440px
and 375px in dark/light themes, plus system reduced motion and stalled-host
recovery. The shared Docker onboarding container was not rebuilt or reset.

## Desktop connection follow-up

Connection preparation now owns synchronization, shares one shell environment,
and reuses a successful unchanged-runtime probe within the mutation lock. Window
readiness no longer triggers another synchronization. Updates and daemon starts
still require fresh readiness checks; later attempts refresh the environment.
The splash waits only for Local, with remote restoration continuing independently.

Validation: 56 focused frontend tests passed; desktop suite 84 passed, 9 skipped;
web production build and desktop typecheck passed. Alternating before/after
`LocalRuntime.prepare` measurements against the running development daemon were
3811/3667 ms before and 2168/1657 ms after (about 49% less time on average).
Measurements used temporary desktop settings, verified matching installed bytes
and daemon build before running, and confirmed the daemon PID stayed unchanged.
These measure connection preparation, not total Electron process startup; the
remaining login-shell read still takes most of that time.
