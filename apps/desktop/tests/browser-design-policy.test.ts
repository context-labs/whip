import test from "node:test";
import assert from "node:assert/strict";
import { themeCatalog } from "@whip/ui/theme-data";
import {
  designDraft,
  designIntent,
  designLease,
  designRevision,
  designImage,
  evidenceURL,
} from "../src/browser-design-policy";
const lease = {
  epoch: "epoch",
  tabId: "tab",
  generation: "gen",
  designId: "design",
};
const draft = {
  prompt: "Change this\n@not-expanded",
  recipients: [
    { id: "host:root:agent", label: "Conversation", available: true },
  ],
  recipientId: "host:root:agent",
  screenshot: true,
  delivery: "queue",
  busy: false,
  uncertain: false,
  theme: { name: "default", mode: "dark" },
};
test("Design closed contracts reject extras, malformed revisions and coordinate floods", () => {
  assert.deepEqual(designLease(lease), lease);
  assert.throws(() => designLease({ ...lease, command: "Runtime.evaluate" }));
  assert.throws(() =>
    designRevision({ ...lease, documentRevision: -1, selectionRevision: 0 }),
  );
  assert.throws(() =>
    designRevision({ ...lease, documentRevision: 1, selectionRevision: NaN }),
  );
  for (const intent of [
    { kind: "pick", x: Infinity, y: 0 },
    { kind: "scroll", x: 0, y: 0, deltaX: 0, deltaY: 1001 },
    { kind: "send", value: "smuggled" },
    { kind: "pick", x: 0, y: 0, additive: 1 },
    { kind: "execute", code: "evil()" },
  ])
    assert.throws(() => designIntent(intent));
  assert.deepEqual(
    designIntent({ kind: "pick", x: 10.25, y: 20, additive: true }),
    { kind: "pick", x: 10.25, y: 20, additive: true },
  );
  assert.deepEqual(designIntent({ kind: "recipient", id: "" }), {
    kind: "recipient",
    id: "",
  });
  assert.throws(() => designIntent({ kind: "remove", id: "" }));
  for (const kind of ["next", "previous", "evidence-close"])
    assert.deepEqual(designIntent({ kind }), { kind });
  assert.deepEqual(designIntent({ kind: "pick-hover", additive: true }), {
    kind: "pick-hover",
    additive: true,
  });
  assert.throws(() => designIntent({ kind: "pick-hover", x: 1 }));
});
test("Design bounded draft/evidence only accepts PNG and explicit unique recipients", () => {
  assert.deepEqual(designDraft(draft), draft);
  assert.equal(designDraft({ ...draft, promptReset: 2 }).promptReset, 2);
  assert.throws(() => designDraft({ ...draft, promptReset: -1 }));
  assert.throws(() => designDraft({ ...draft, prompt: "x".repeat(16385) }));
  assert.throws(() =>
    designDraft({
      ...draft,
      recipients: [draft.recipients[0], draft.recipients[0]],
    }),
  );
  assert.throws(() =>
    designDraft({ ...draft, evidence: { text: "x".repeat(65537) } }),
  );
  assert.throws(() => designImage("data:image/svg+xml,<svg/>"));
  assert.throws(() =>
    designDraft({
      ...draft,
      theme: { name: "default", mode: "dark", css: "body{}" },
    }),
  );
});
test("Design custom palette and display are validated data, never CSS", () => {
  const palette = {
    ...themeCatalog[0],
    id: "custom:" + "界".repeat(100),
    name: "My palette",
  };
  const display = {
    uiFont: "system",
    codeFont: "system",
    uiSize: 16,
    codeSize: 14,
    wrapCode: true,
    contrast: "more",
    motion: "reduce",
  };
  const value = designDraft({
    ...draft,
    theme: { name: palette.id, mode: "dark", palette, display },
  });
  assert.deepEqual(value.theme.palette, palette);
  assert.deepEqual(value.theme.display, display);
  assert.throws(() =>
    designDraft({
      ...draft,
      theme: {
        ...draft.theme,
        palette: {
          ...palette,
          colors: {
            ...palette.colors,
            background: "url(https://attacker.test)",
          },
        },
      },
    }),
  );
  assert.throws(() =>
    designDraft({
      ...draft,
      theme: {
        ...draft.theme,
        display: { ...display, uiFont: "url(https://attacker.test)" },
      },
    }),
  );
});
test("Design evidence URL strips authority credentials, queries and fragments", () => {
  assert.equal(
    evidenceURL("https://user:password@example.test/path?token=secret#private"),
    "https://example.test/path",
  );
  assert.equal(evidenceURL("not a URL"), "");
});
