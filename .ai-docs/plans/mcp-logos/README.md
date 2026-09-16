# MCP server logos

Branch: `mcp-logos` (cut from `compaction-loop-and-ui-cleanup` after PR #152)

## Why

The import screen shipped with a one-letter monogram per server (plan
`mcp-import-onboarding`, decision 8: "icons are a later change; the daemon
still sends `BrandHint` so that change is web-only"). The Paper design the
screen was built from (frame J7F-1) shows company marks in the tile, and a
list of `ahrefs`, `exa`, `figma`, `paper` reads far faster with them.

Decision 8 also said "no favicon fetching, ever", because the list of servers
on a machine should not leak to third parties to draw icons. Sam reversed
that on 2026-09-15 and asked for the logo to come from the server's URL
through a service, with a good placeholder when nothing resolves. This plan
keeps the leak small and visible: the common vendors ship inside the app and
never leave the machine; only domains outside that set are looked up, by one
service, once per host per month, behind a switch shown in Settings.

Nothing existing changes behaviour: the import screen, `whip mcp import`, the
candidates and apply operations, and the CSP all stay as they are. Icons are
decoration layered on top; a host that cannot resolve any keeps the monogram.

## Decisions (Sam, 2026-09-15)

1. **Bundle the 200.** Executor's integration catalogue lists 1,357 MCP
   entries behind exactly 200 registrable domains; the app ships one 64 px
   mark for each, fetched once from `integrations.sh/logo/{domain}` by
   `scripts/mcp-brands.mjs` and checked in as data URIs. No run-time traffic
   for these, no favicon crawling, no simple-icons.
2. **DuckDuckGo for the rest** (`icons.duckduckgo.com/ip3/{domain}.ico`):
   no key, no attribution, a clean 404 on a miss. logo.dev (key plus
   attribution) and Brandfetch (terms forbid programmatic access and caching)
   are out. On by default; `brandIcons: false` turns it off.
3. **Placeholder: tinted first-letter monogram.** Five theme-aware tints
   chosen by a stable hash of the name; native and unsupported rows keep the
   quiet grey they had.
4. **Import screen only.** `MCPBrandIcon` is a plain component the Host
   integrations list can adopt later.
5. **No vendor-site crawling and no MCP-declared icons in this change.**
   Parsing `<link rel>` from 200 front pages is what integrations.sh already
   did once for the bundle; `serverInfo.icons` (spec 2025-11-25) is set by
   almost no server today and would need a per-session hook. Both are
   follow-ups if the long tail turns out to matter.

## What shipped

Every row of the import screen shows a 22 px mark in the tile that used to
hold the first letter, from the first source that answers:

1. **Bundled marks** (web, no network): `packages/app/src/assets/mcp-brands.json`,
   199 entries keyed by registrable domain (granola.ai's mark is a 22 KB SVG
   and was left out), loaded as one lazy chunk when the screen mounts.
2. **DuckDuckGo via the daemon** (`mcp.brand.icons`): for rows whose
   `brand_key` the bundle lacks. The daemon caches hits for 30 days and
   misses for 3 under `~/.whip/icons`, so a domain is sent at most once per
   host per month.
3. **Monogram**: the tinted placeholder.

### Brand key

`brandKey` in `internal/mcp/import.go`: `publicsuffix.EffectiveTLDPlusOne`
of the URL host, lower-cased (`mcp.figma.com` → `figma.com`,
`api.ahrefs.com` → `ahrefs.com`, `team.github.io` → `team.github.io`). IP
literals, single-label hosts and `.local`, `.localhost`, `.internal`,
`.lan`, `.home.arpa`, `.ts.net` yield `""`, as do stdio servers: a package
name is not a domain, so Playwright or Chrome DevTools rows show the
monogram. `golang.org/x/net` was already a dependency; `publicsuffix` made
it direct. Carried as `brand_key` on `MCPImportCandidate` beside
`brand_hint`.

### `internal/brandicon`

`Resolver.Resolve(ctx, keys) map[string]string` returns data URIs. Rules:
keys must be lower-case domains with an alphabetic top label (anything else
is ignored before any request); `https` fixed host; no redirects followed;
4 s per request, 8 s per call from the daemon; `io.LimitReader` at 48 KiB;
type by `http.DetectContentType` against png/jpeg/gif/webp/ico (SVG refused);
`User-Agent: whip`, no cookies or auth. 404/400 are remembered misses; other
failures are not remembered. Per-key close-to-broadcast channel so concurrent
callers share one fetch; four fetches in flight at most. Cache files are
`0o600` in a `0o700` directory, tmp+rename, capped at 512 entries oldest
first. `Endpoint` is a package variable so tests point it at `httptest`.

### Protocol, daemon, config

- `mcp.brand.icons` (`Query`, `host-configuration`): `{keys}` → `{icons}`,
  at most 64 keys. Registered beside `mcp.import.*`, dispatched in
  `provider_rpc.go`, implemented on `ProviderService` in
  `mcp_import_service.go` with a lazily created resolver under
  `config.Dir()/icons`.
- `config.BrandIcons *bool` (`brandIcons`; nil is on) → `RuntimeConfiguration.brand_icons`
  and `ConfigurationUpdate.brand_icons`.
- Settings › Agents & execution › MCP servers gains a "Server logos" switch
  (`settings/mcp-import.tsx`) that saves on its own against the current
  revision; the navigation index lists it as `mcp_logos`.

### Web

`packages/app/src/mcp-brand.tsx`: `useBrandMarks` (lazy JSON chunk, pinned
`gcTime`), `useBrandIcons(client, keys)` (gated on `client.supports`, 30 min
cache), `tintIndex`, `MCPBrandIcon({name, src, quiet})` with `onError` →
monogram. `mcp-import.tsx` asks the daemon only for keys the bundle lacks and
only after the bundle has loaded. CSP untouched: everything is a `data:` URI.

## Tests

- Go: `TestBrandKey` (table plus candidates), `internal/brandicon`
  (cache hits and misses across restarts, the refusal table, invalid keys
  make no request, sixteen concurrent callers share one fetch, cancellation is
  not remembered, bounded and private cache), `-race` clean;
  `TestMCPBrandIconsHonourTheHostSwitch` (default on, `127.0.0.1` never
  asked, cache under `WHIP_HOME/icons`, off answers nothing and dials nothing,
  `config.get` reflects it, 65 keys refused); boundaries, parity and drift
  pick the operation up.
- Web: `mcp-brand.test.tsx` (mark, decode failure, monogram, stable tints,
  the bundle's shape), `mcp-import.test.tsx` (bundled marks at once, the
  daemon asked only for the rest, monogram for a server without a domain),
  `settings-mcp-import.test.tsx` (the switch saves against the revision),
  `workflow-inventory` row, `settings-navigation` entry. Full `test:web`
  green.

## Research (kept for the record)

- Executor (`/Applications/Executor.app`, `~/.executor/cache/integrations.json`,
  5,980 integrations, 1,357 of kind `mcp` over 200 domains) ships a catalogue
  with `icon: https://integrations.sh/logo/{domain}`; it does not crawl at run
  time. integrations.sh serves 64 px JPEG/PNG/SVG marks and a 400 on unknown
  domains; its terms are not published on the site.
- Gator shows Apollo's `logoUrl` in a rounded box with a `Building2`
  fallback; no resolver.
- Clearbit's logo API shut down on 8 Dec 2025. logo.dev: publishable key,
  500K/month free, attribution on the free plan, monogram or `fallback=404`.
  Brandfetch: client id, 1M/month, direct `<img>` embedding only. DuckDuckGo:
  keyless 32 px PNG, 404 on a miss. Google S2: keyless but a miss is a globe
  with status 200. Guessing `/apple-touch-icon.png` hit 1 of 5 vendors.
- MCP 2025-11-25 (SEP-973) adds `icons` to `Implementation`; the spec's icon
  security list (https/data only, same origin, no credentials, sniff, cap)
  shaped the resolver's rules. Claude.ai ignores `serverInfo.icons` today
  (anthropics/claude-ai-mcp#152).
- Whip's CSP is `img-src 'self' data: blob:` in three pinned places; data
  URIs were the only design that leaves it alone.

## Follow-ups

- Vendor-site `<link rel>` parsing for domains DuckDuckGo misses, and
  `serverInfo.icons` for native servers, if the long tail matters.
- The Host integrations list could reuse `MCPBrandIcon` with the same
  `brand_key`.
- Refresh the bundle with `node scripts/mcp-brands.mjs` when the domain list
  changes; the list itself came from Executor's catalogue and can be edited
  by hand.
