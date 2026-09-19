// Run after npm run build:web: actual app overlay + native controller/preload/IPC.
// Set BROWSER_DESIGN_DIST to validate an isolated final renderer build.
// BROWSER_DESIGN_SHORTCUT_ONLY=1 runs focused guest/overlay keyboard gates without motion/compositor gates.
import { build } from "esbuild";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { once } from "node:events";
import electron from "electron";
const directory = await mkdtemp(path.join(tmpdir(), "whip-design-production-"));
let child;
try {
  await build({
    entryPoints: [
      "apps/desktop/scripts/browser-design-production-main.ts",
      "apps/desktop/src/browser-design-preload.ts",
      "apps/desktop/src/preload.ts",
    ],
    outdir: directory,
    bundle: true,
    platform: "node",
    format: "cjs",
    external: ["electron"],
    outExtension: { ".js": ".cjs" },
    entryNames: "[name]",
    define: { __APP_VERSION__: '"native-test"', __APP_NAME__: '"Whip"' },
  });
  child = spawn(
    electron,
    [path.join(directory, "browser-design-production-main.cjs")],
    {
      stdio: "inherit",
      env: { ...process.env, BROWSER_DESIGN_DIRECTORY: directory },
    },
  );
  const timer = setTimeout(() => child.kill("SIGKILL"), 45000);
  timer.unref();
  const [code, signal] = await once(child, "exit");
  clearTimeout(timer);
  if (code !== 0)
    throw new Error(`Production Design fixture exited ${code ?? signal}`);
} finally {
  if (child?.exitCode === null && child?.signalCode === null)
    child.kill("SIGKILL");
  await rm(directory, { recursive: true, force: true });
}
