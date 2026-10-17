import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const blocking = new Set(["high", "critical"]);
const severities = new Set(["info", "low", "moderate", "high", "critical"]);

// Pending upstream fixes, not registry releases. Keep these independent of the
// manifest so replacing an override cannot silently preserve the audit exception.
// Provenance and removal instructions: docs/dependency-security.md.
export const securityPins = {
  braces: {
    version: "3.0.3",
    resolved: "https://codeload.github.com/micromatch/braces/tar.gz/a795ee83ecd011131b2d618fe754ac5a8e124dae",
    integrity: "sha512-w/zrxDVIuC1EaZHrMyq3LyFTCwQ/+badAhpR+FfC8Me0qBPxJ3zSWg5ux3fwZMjVyUUwM9H1tQU7Sj2pAwZuqA==",
    files: {
      "index.js": "332ea07c7b006361aad12aa994ca75dc1db8e8382b884909e2f38f10b85c88a4",
      "lib/assert-depth.js": "20642e4be21253e8269e42511d61f076fcac2903c27c94f7b1194f182d6b36dd",
      "lib/compile.js": "0527119921a3117bfbb7a3a86d400bcaf26c3988628688301ee17c5cc0003719",
      "lib/expand.js": "bdcde9b36e94d248c5035ab8f98f4a20189c973fa44ee68cd576ba7ebf28022b",
      "lib/parse.js": "32702456744324649a1a29ae234e08eb392d3db53dd7a5c872de808bba03c6fb",
      "lib/stringify.js": "056ab10d759e5d2ca81fc888b122acf450c692b5e409dc19a41aca06aba8f2ef",
    },
  },
  "node-forge": {
    version: "1.4.1-0",
    resolved: "https://codeload.github.com/digitalbazaar/forge/tar.gz/ceba34402e329f0365134f23fe19898756527d65",
    integrity: "sha512-fYRFJf8ymZNJ5jGirbhuwAljM0rSJG03cldnAK3sJRMDHSS+tG8DPosOB8he34pGWMylVCMOtnIxUEepsME2Rg==",
    files: {
      "lib/rsa.js": "acc22e5d36e27832c34e02dd3933aad7977d45b047eead5016520735efedc9c5",
    },
  },
};

export function verifySecurityPins(directory = root) {
  directory = realpathSync(directory);
  const manifest = JSON.parse(readFileSync(join(directory, "package.json"), "utf8"));
  const lock = JSON.parse(readFileSync(join(directory, "package-lock.json"), "utf8"));
  const require = createRequire(join(directory, "package.json"));
  for (const [name, pin] of Object.entries(securityPins)) {
    const location = `node_modules/${name}`;
    const copies = Object.keys(lock.packages).filter((path) =>
      path === location || path.endsWith(`/${location}`));
    assert.deepEqual(copies, [location], `Unexpected copies of ${name}`);
    assert.equal(manifest.overrides?.[name], pin.resolved, `${name} override changed`);
    for (const field of ["version", "resolved", "integrity"]) {
      assert.equal(lock.packages[location][field], pin[field], `${name} lock ${field} changed`);
    }
    const packageFile = join(directory, location, "package.json");
    assert.equal(require.resolve(`${name}/package.json`), packageFile);
    assert.equal(JSON.parse(readFileSync(packageFile, "utf8")).version, pin.version);
    for (const [file, expected] of Object.entries(pin.files)) {
      const actual = createHash("sha256").update(readFileSync(join(directory, location, file))).digest("hex");
      assert.equal(actual, expected, `${name}/${file} does not contain the reviewed fix`);
    }
  }
}

function isPatchedBracesFinding(finding) {
  return finding.name === "braces" && finding.dependency === "braces"
    && finding.source === 1240992 && finding.range === "<=3.0.3"
    && finding.severity === "high"
    && finding.url === "https://github.com/advisories/GHSA-vfj7-8cjw-p6xm";
}

export function classifyAudit(report) {
  assert.equal(report.auditReportVersion, 2, "Unsupported npm audit report");
  assert.ok(!report.error, "npm audit reported an error");
  const vulnerabilities = report.vulnerabilities;
  assert.ok(vulnerabilities && typeof vulnerabilities === "object" && !Array.isArray(vulnerabilities));
  for (const [name, entry] of Object.entries(vulnerabilities)) {
    assert.equal(entry.name, name);
    assert.ok(severities.has(entry.severity) && Array.isArray(entry.via), `Malformed finding: ${name}`);
    for (const via of entry.via) {
      if (typeof via === "string") {
        assert.ok(Object.hasOwn(vulnerabilities, via), `Missing audit dependency: ${via}`);
      } else {
        assert.ok(via && typeof via.name === "string" && typeof via.url === "string"
          && severities.has(via.severity), `Malformed advisory: ${name}`);
      }
    }
  }

  const patched = new Map();
  const unresolved = new Map();
  for (const [name, entry] of Object.entries(vulnerabilities)) {
    if (!blocking.has(entry.severity)) continue;
    const pending = [name];
    const seen = new Set();
    let explained = false;
    while (pending.length) {
      const dependency = pending.pop();
      if (seen.has(dependency)) continue;
      seen.add(dependency);
      for (const via of vulnerabilities[dependency].via) {
        if (typeof via === "string") {
          pending.push(via);
        } else if (blocking.has(via.severity)) {
          explained = true;
          const findings = isPatchedBracesFinding(via) ? patched : unresolved;
          findings.set(`${via.name}:${via.url}`, via);
        }
      }
    }
    assert.ok(explained, `Unexplained blocking audit finding: ${name}`);
  }
  return { patched: [...patched.values()], unresolved: [...unresolved.values()] };
}

function main() {
  verifySecurityPins();
  const result = spawnSync("npm", ["audit", "--omit=dev", "--audit-level=high", "--json"], {
    cwd: root, encoding: "utf8", maxBuffer: 16 * 1024 * 1024,
  });
  if (result.error) throw result.error;
  assert.ok(result.status === 0 || result.status === 1, result.stderr || "npm audit failed");
  const report = JSON.parse(result.stdout);
  const findings = classifyAudit(report);
  if (result.status === 1) {
    assert.ok(findings.patched.length + findings.unresolved.length > 0, "Unexplained npm audit failure");
  }
  for (const finding of findings.patched) {
    console.log(`Verified patched upstream source: ${finding.name} ${finding.url}`);
  }
  for (const finding of findings.unresolved) {
    console.error(`${finding.severity}: ${finding.name} — ${finding.url}`);
  }
  console.log(`Production audit: ${findings.unresolved.length} unremediated high/critical advisories; ${findings.patched.length} verified patched advisory.`);
  console.log("Raw npm audit dependency counts:", JSON.stringify(report.metadata?.vulnerabilities));
  if (findings.unresolved.length) process.exitCode = 1;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
