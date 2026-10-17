import assert from "node:assert/strict";
import { constants, createHash, generateKeyPairSync, privateEncrypt, sign } from "node:crypto";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import braces from "braces";
import forge from "node-forge";
import { classifyAudit, securityPins, verifySecurityPins } from "./audit-production.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const bracesFinding = {
  source: 1240992, name: "braces", dependency: "braces", range: "<=3.0.3",
  severity: "high", url: "https://github.com/advisories/GHSA-vfj7-8cjw-p6xm",
};
const report = (finding = bracesFinding) => ({
  auditReportVersion: 2,
  vulnerabilities: {
    braces: { name: "braces", severity: "high", via: [finding] },
    consumer: { name: "consumer", severity: "high", via: ["braces", "consumer"] },
  },
});

test("installed dependencies contain exactly the reviewed upstream fixes", () => verifySecurityPins());

test("audit recognizes only the fixed advisory, including transitive and cyclic paths", () => {
  const result = classifyAudit(report());
  assert.equal(result.patched.length, 1);
  assert.equal(result.unresolved.length, 0);
});

test("new or revised advisories still block", () => {
  for (const change of [
    { url: "https://github.com/advisories/GHSA-new-advisory" },
    { source: 9999999 }, { range: "*" }, { severity: "critical" },
    { name: "another-package", dependency: "another-package" },
  ]) {
    const result = classifyAudit(report({ ...bracesFinding, ...change }));
    assert.equal(result.patched.length, 0);
    assert.equal(result.unresolved.length, 1);
  }
  const mixed = report();
  mixed.vulnerabilities.braces.via.push({ ...bracesFinding, url: "https://github.com/advisories/GHSA-new-advisory" });
  assert.equal(classifyAudit(mixed).unresolved.length, 1);
});

test("malformed or incomplete audit reports fail closed", () => {
  for (const invalid of [
    {}, { ...report(), error: { message: "registry unavailable" } },
    { ...report(), auditReportVersion: 3 },
    { auditReportVersion: 2, vulnerabilities: { consumer: { name: "consumer", severity: "high", via: ["missing"] } } },
    { auditReportVersion: 2, vulnerabilities: { consumer: { name: "consumer", severity: "high", via: ["consumer"] } } },
  ]) assert.throws(() => classifyAudit(invalid));
  assert.deepEqual(classifyAudit({ auditReportVersion: 2, vulnerabilities: {} }), { patched: [], unresolved: [] });
});

test("audit rejects registry fallback, duplicate copies, and altered patch bytes", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "whip-dependency-pins-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  for (const file of ["package.json", "package-lock.json"]) cpSync(join(root, file), join(directory, file));
  for (const [name, pin] of Object.entries(securityPins)) {
    for (const file of ["package.json", ...Object.keys(pin.files)]) {
      const destination = join(directory, "node_modules", name, file);
      mkdirSync(dirname(destination), { recursive: true });
      cpSync(join(root, "node_modules", name, file), destination);
    }
  }
  verifySecurityPins(directory);
  const lockFile = join(directory, "package-lock.json");
  const original = readFileSync(lockFile, "utf8");
  const lock = JSON.parse(original);
  lock.packages["node_modules/braces"].resolved = "https://registry.npmjs.org/braces/-/braces-3.0.3.tgz";
  writeFileSync(lockFile, JSON.stringify(lock));
  assert.throws(() => verifySecurityPins(directory), /lock resolved changed/);
  writeFileSync(lockFile, original);
  const duplicate = JSON.parse(original);
  duplicate.packages["node_modules/example/node_modules/braces"] = duplicate.packages["node_modules/braces"];
  writeFileSync(lockFile, JSON.stringify(duplicate));
  assert.throws(() => verifySecurityPins(directory), /Unexpected copies/);
  writeFileSync(lockFile, original);
  writeFileSync(join(directory, "node_modules/braces/lib/assert-depth.js"), "module.exports = () => {};\n");
  assert.throws(() => verifySecurityPins(directory), /does not contain the reviewed fix/);
});

test("braces rejects deep strings and ASTs before recursive stack exhaustion", () => {
  const deep = "{".repeat(4000) + "a,b" + "}".repeat(4000);
  assert.throws(() => braces(deep), SyntaxError);
  let ast = { type: "text", value: "x" };
  for (let i = 0; i < 12000; i++) ast = { type: "brace", nodes: [ast] };
  for (const operation of [braces.compile, braces.expand, braces.stringify]) {
    assert.throws(() => operation(ast), SyntaxError);
    const cyclic = { type: "brace", nodes: [] };
    cyclic.nodes.push(cyclic);
    assert.throws(() => operation(cyclic), /acyclic/);
  }
  assert.deepEqual(braces.expand("{a,b}{1..3}"), ["a1", "a2", "a3", "b1", "b2", "b3"]);
  assert.doesNotThrow(() => braces("{".repeat(64) + "a,b" + "}".repeat(64)));
});

test("forge accepts valid signatures and rejects extra nested DigestAlgorithm elements", () => {
  const keys = generateKeyPairSync("rsa", { modulusLength: 1024 });
  const publicKey = forge.pki.publicKeyFromPem(keys.publicKey.export({ type: "spki", format: "pem" }));
  const message = Buffer.from("whip dependency security regression");
  const digest = createHash("sha256").update(message).digest();
  assert.equal(publicKey.verify(digest.toString("binary"), sign("sha256", message, keys.privateKey).toString("binary")), true);

  // SHA-256 DigestInfo with an extra OCTET STRING inside DigestAlgorithm.
  // The unchanged registry release accepts this mathematically valid signature;
  // the pinned fix must reject its malformed ASN.1 structure.
  const digestInfo = Buffer.concat([Buffer.from("30343010060960864801650304020105000401000420", "hex"), digest]);
  const encoded = Buffer.concat([Buffer.from([0, 1]), Buffer.alloc(128 - digestInfo.length - 3, 0xff), Buffer.from([0]), digestInfo]);
  const malformed = privateEncrypt({ key: keys.privateKey, padding: constants.RSA_NO_PADDING }, encoded);
  assert.throws(() => publicKey.verify(digest.toString("binary"), malformed.toString("binary")), /DigestInfo/);
});
