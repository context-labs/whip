# Bundled QuickJS WASM

`quickjs.wasm` is the unmodified 636,400-byte binary from the
[`quickjs-wasi@3.6.0` npm distribution](https://www.npmjs.com/package/quickjs-wasi/v/3.6.0).
Its SHA-256 is `b006d95d9475edf7c6648cc3eb391d3b780efdd99022fbfbb470f2359da460ff`.
The npm archive's SHA-512 integrity is
`/CNzMq42B8ThzUpzjLSF8K50ntVLV3RREyT8zr4wHaP2CZSHFhPkqg3E1GULmTerehf8Bo+UguQ+4LoOMCTC6A==`.
Both the archive integrity and embedded binary equality were verified during integration.

The package declares the MIT license. Its npm provenance identifies source commit
[`54c4d2dd4be2445409aeab603ecfc3bb209c7310`](https://github.com/vercel-labs/quickjs-wasi/tree/54c4d2dd4be2445409aeab603ecfc3bb209c7310)
and [release build](https://github.com/vercel-labs/quickjs-wasi/actions/runs/32903780452/attempts/1).
That source pins the QuickJS-NG submodule to
[`65641a0c1e85cc266d7613d6673a22ec834bb941`](https://github.com/quickjs-ng/quickjs/tree/65641a0c1e85cc266d7613d6673a22ec834bb941).
Its complete MIT notice is retained in [LICENSE.quickjs-ng](LICENSE.quickjs-ng).
The package repository does not supply a separate license file for its interface
layer; its `package.json` and README declare MIT.

Reproduction: download the exact npm archive at
`https://registry.npmjs.org/quickjs-wasi/-/quickjs-wasi-3.6.0.tgz`, verify the
SHA-512 above, then extract only `package/quickjs.wasm` and verify its SHA-256.
WHIP always embeds this binary; neither users nor model code select a WASM path.
No optional native extensions from the package are bundled or loaded.

The Go host uses `github.com/tetratelabs/wazero v1.12.0` (Apache-2.0), as pinned in
WHIP's module files. The bridge derives from the local approved QuickJS research
prototype, adapted to WHIP's settled-cell profile and authority-free worker.
