import type {
  BrowserDesignDraft,
  BrowserDesignIntent,
  BrowserDesignLease,
  BrowserDesignRevision,
} from "@whip/app/desktop-bridge";
import { object, target } from "./browser-policy";
import { validateTheme, validateDisplayPreferences } from "@whip/ui/theme-data";

export const designLimits = {
  selections: 16,
  prompt: 16_384,
  metadata: 65_536,
  image: 12 * 1024 * 1024,
  recipients: 100,
};
export function designText(value: unknown, max: number, empty = true): string {
  if (
    typeof value !== "string" ||
    (!empty && !value) ||
    Buffer.byteLength(value, "utf8") > max ||
    /[\u0000-\u0008\u000b\u000c\u000e-\u001f]/u.test(value)
  )
    throw new Error("Invalid Design text");
  return value;
}
export function designInteger(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    throw new Error("Invalid Design revision");
  return value;
}
export function designLease(
  value: unknown,
  extra: string[] = [],
): BrowserDesignLease {
  const input = object(value, [
    "epoch",
    "tabId",
    "generation",
    "designId",
    ...extra,
  ]);
  return {
    ...target({
      epoch: input.epoch,
      tabId: input.tabId,
      generation: input.generation,
    }),
    designId: designText(input.designId, 128, false),
  };
}
export function designRevision(
  value: unknown,
  extra: string[] = [],
): BrowserDesignRevision {
  const input = object(value, [
    "epoch",
    "tabId",
    "generation",
    "designId",
    "documentRevision",
    "selectionRevision",
    ...extra,
  ]);
  return {
    ...designLease(input, ["documentRevision", "selectionRevision", ...extra]),
    documentRevision: designInteger(input.documentRevision),
    selectionRevision: designInteger(input.selectionRevision),
  };
}
export function designBoolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw new Error("Invalid Design flag");
  return value;
}
export function designDraft(value: unknown): BrowserDesignDraft {
  const input = object(value, [
    "prompt",
    "recipients",
    "recipientId",
    "screenshot",
    "delivery",
    "busy",
    "uncertain",
    "error",
    "evidence",
    "theme",
    "promptReset",
  ]);
  if (
    !Array.isArray(input.recipients) ||
    input.recipients.length > designLimits.recipients
  )
    throw new Error("Too many Design recipients");
  const recipients = input.recipients.map((value) => {
    const item = object(value, ["id", "label", "available"]);
    return {
      id: designText(item.id, 512, false),
      label: designText(item.label, 512, false),
      available: designBoolean(item.available),
    };
  });
  if (new Set(recipients.map((item) => item.id)).size !== recipients.length)
    throw new Error("Duplicate Design recipients");
  if (input.delivery !== "queue" && input.delivery !== "steer")
    throw new Error("Invalid Design delivery");
  const theme = object(input.theme, ["name", "mode", "palette", "display"]);
  if (theme.mode !== "light" && theme.mode !== "dark")
    throw new Error("Invalid Design theme");
  let evidence: BrowserDesignDraft["evidence"];
  if (input.evidence !== undefined) {
    const value = object(input.evidence, ["text", "image"]);
    evidence = {
      text: designText(value.text, designLimits.metadata),
      ...(value.image !== undefined ? { image: designImage(value.image) } : {}),
    };
  }
  return {
    ...(input.promptReset !== undefined
      ? { promptReset: designInteger(input.promptReset) }
      : {}),
    prompt: designText(input.prompt, designLimits.prompt),
    recipients,
    recipientId: designText(input.recipientId, 512),
    screenshot: designBoolean(input.screenshot),
    delivery: input.delivery,
    busy: designBoolean(input.busy),
    uncertain: designBoolean(input.uncertain),
    theme: {
      name: designText(theme.name, 1024, false),
      mode: theme.mode,
      ...(theme.palette !== undefined
        ? { palette: validateTheme(theme.palette) }
        : {}),
      ...(theme.display !== undefined
        ? { display: validateDisplayPreferences(theme.display) }
        : {}),
    },
    ...(input.error !== undefined
      ? { error: designText(input.error, 2048) }
      : {}),
    ...(evidence ? { evidence } : {}),
  };
}
export function designImage(value: unknown): string {
  const image = designText(value, designLimits.image);
  if (!/^data:image\/png;base64,[a-zA-Z0-9+/=]+$/.test(image))
    throw new Error("Invalid Design image");
  return image;
}
export function designIntent(value: unknown): BrowserDesignIntent {
  const base = object(value, [
    "kind",
    "x",
    "y",
    "additive",
    "deltaX",
    "deltaY",
    "id",
    "value",
  ]);
  const kind = base.kind;
  const finite = (value: unknown, max: number) => {
    if (
      typeof value !== "number" ||
      !Number.isFinite(value) ||
      Math.abs(value) > max
    )
      throw new Error("Invalid Design coordinate");
    return value;
  };
  if (kind === "hover" || kind === "pick") {
    object(value, ["kind", "x", "y", "additive"]);
    return {
      kind,
      x: finite(base.x, 32768),
      y: finite(base.y, 32768),
      ...(base.additive !== undefined
        ? { additive: designBoolean(base.additive) }
        : {}),
    };
  }
  if (kind === "scroll") {
    object(value, ["kind", "x", "y", "deltaX", "deltaY"]);
    return {
      kind,
      x: finite(base.x, 32768),
      y: finite(base.y, 32768),
      deltaX: finite(base.deltaX, 1000),
      deltaY: finite(base.deltaY, 1000),
    };
  }
  if (kind === "remove" || kind === "recipient") {
    object(value, ["kind", "id"]);
    return { kind, id: designText(base.id, 512, kind === "recipient") };
  }
  if (kind === "prompt") {
    object(value, ["kind", "value"]);
    return { kind, value: designText(base.value, designLimits.prompt) };
  }
  if (kind === "screenshot") {
    object(value, ["kind", "value"]);
    return { kind, value: designBoolean(base.value) };
  }
  if (kind === "delivery") {
    object(value, ["kind", "value"]);
    if (base.value !== "queue" && base.value !== "steer")
      throw new Error("Invalid Design delivery");
    return { kind, value: base.value };
  }
  if (kind === "pick-hover") {
    object(value, ["kind", "additive"]);
    return {
      kind,
      ...(base.additive !== undefined
        ? { additive: designBoolean(base.additive) }
        : {}),
    };
  }
  if (
    kind === "capture" ||
    kind === "send" ||
    kind === "stop" ||
    kind === "clear" ||
    kind === "ancestor" ||
    kind === "next" ||
    kind === "previous" ||
    kind === "evidence-close"
  ) {
    object(value, ["kind"]);
    return { kind };
  }
  throw new Error("Unsupported Design intent");
}
export function evidenceURL(raw: string): string {
  try {
    const url = new URL(raw);
    url.username = "";
    url.password = "";
    url.search = "";
    url.hash = "";
    return url.toString().slice(0, 2048);
  } catch {
    return "";
  }
}
