// Native Design Mode feasibility: isolated fixture, no daemon or production app.
import { build } from "esbuild";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { once } from "node:events";
import electron from "electron";
const directory = await mkdtemp(path.join(tmpdir(), "whip-browser-design-"));
let child;
try {
  await build({
    entryPoints: [
      "apps/desktop/scripts/browser-design-native-main.ts",
      "apps/desktop/scripts/browser-design-native-preload.ts",
    ],
    outdir: directory,
    bundle: true,
    platform: "node",
    format: "cjs",
    external: ["electron"],
    outExtension: { ".js": ".cjs" },
    entryNames: "[name]",
  });
  if (process.platform === "darwin") {
    const compiler = spawn(
      "xcrun",
      [
        "swiftc",
        "apps/desktop/scripts/browser-design-native-input.swift",
        "-o",
        path.join(directory, "native-input"),
      ],
      { stdio: "inherit" },
    );
    const [code] = await once(compiler, "exit");
    if (code !== 0) throw new Error("Native input fixture compilation failed");
  }
  console.log("DESIGN_SPIKE_DIRECTORY", directory);
  child = spawn(
    electron,
    [path.join(directory, "browser-design-native-main.cjs")],
    {
      stdio: "inherit",
      env: { ...process.env, BROWSER_DESIGN_DIRECTORY: directory },
    },
  );
  const timeout = setTimeout(() => child.kill("SIGKILL"), 40000);
  timeout.unref();
  const [code, signal] = await once(child, "exit");
  clearTimeout(timeout);
  if (code !== 0)
    throw new Error(`Design native spike exited ${code ?? signal}`);
} finally {
  if (child?.exitCode === null && child?.signalCode === null)
    child.kill("SIGKILL");
  if (process.env.BROWSER_DESIGN_KEEP !== "1")
    await rm(directory, { recursive: true, force: true });
}
