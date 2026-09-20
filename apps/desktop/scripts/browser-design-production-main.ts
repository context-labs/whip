import type { BrowserEvent, BrowserTarget } from '@whip/app/desktop-bridge';
import { app, BrowserWindow, WebContentsView, nativeImage } from "electron";
import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { readFileSync } from "node:fs";
import { once } from "node:events";
import { BrowserManager } from "../src/browser-manager";
import { BrowserDesignController } from "../src/browser-design";
import { installBrowserDesignIPC } from "../src/browser-design-ipc";
import type {
  BrowserDesignEvent,
  BrowserDesignState,
  BrowserDesignRevision,
} from "@whip/app/desktop-bridge";
const directory = process.env.BROWSER_DESIGN_DIRECTORY!;
const rendererDist = path.resolve(
  process.env.BROWSER_DESIGN_DIST ?? "apps/web/dist",
);
console.log("DESIGN_RENDERER_DIST", rendererDist);
app.setPath("userData", path.join(directory, "profile"));
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
const pass = (message: string) => console.log("PASS", message);
const revision = (state: BrowserDesignState): BrowserDesignRevision => ({
  epoch: state.epoch,
  tabId: state.tabId,
  generation: state.generation,
  designId: state.designId,
  documentRevision: state.documentRevision,
  selectionRevision: state.selectionRevision,
});
const lease = (state: BrowserDesignState) => ({
  epoch: state.epoch,
  tabId: state.tabId,
  generation: state.generation,
  designId: state.designId,
});
let window: BrowserWindow,
  manager: BrowserManager,
  design: BrowserDesignController,
  cleanup: () => void;
const events: BrowserDesignEvent[] = [];
const server = http.createServer((request, response) => {
  const route = new URL(request.url!, "http://fixture").pathname;
  if (route === "/design.html" || route.startsWith("/assets/")) {
    if (route.includes("..")) {
      response.writeHead(403).end();
      return;
    }
    response.setHeader(
      "Content-Type",
      route.endsWith(".js")
        ? "text/javascript"
        : route.endsWith(".css")
          ? "text/css"
          : route.endsWith(".woff2")
            ? "font/woff2"
            : "text/html",
    );
    response.setHeader(
      "Content-Security-Policy",
      "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'none'",
    );
    response.end(readFileSync(path.resolve(rendererDist, "." + route)));
    return;
  }
  response.setHeader("Content-Type", "text/html");
  if (route === "/app") {
    response.end(
      "<!doctype html><title>Trusted app fixture</title>Application",
    );
    return;
  }
  response.end(
    `<!doctype html><style>body{margin:0;background:#dfe8f0;height:1600px;font:20px sans-serif}#first{position:absolute;left:80px;top:60px;width:160px;height:80px}#second{position:absolute;left:300px;top:60px;width:160px;height:80px}#privacy{position:absolute;left:80px;top:240px;width:400px;height:100px}.hidden{display:none}</style><button id=first>Alpha</button><button id=second>Beta</button><section id=privacy>Visible evidence<span class=hidden><b>CSS_HIDDEN_SECRET</b></span><span hidden>HIDDEN_SECRET</span><input type=password value=PASSWORD_SECRET><textarea>TEXTAREA_SECRET</textarea><div contenteditable>EDITABLE_SECRET</div></section><script>window.clicks=0;document.addEventListener('click',()=>window.clicks++);Element.prototype.getBoundingClientRect=function(){throw Error('page override must not execute')};</script>`,
  );
});
async function shortcutChecks(window: BrowserWindow, manager: BrowserManager, design: BrowserDesignController, target: BrowserTarget, guest: Electron.WebContents, shortcuts: BrowserEvent[]) {
  // Main routes only; the app fixture separately proves the existing toggle owner.
  const chord = { type: 'keyDown' as const, keyCode: 'D', modifiers: ['meta', 'shift'] };
  const toggleCount = () => shortcuts.filter(event => event.kind === 'shortcut' && event.shortcut === 'design-toggle').length;
  const key = async (contents: Electron.WebContents, modifiers = chord.modifiers) => {
    contents.sendInputEvent({ ...chord, modifiers });
    contents.sendInputEvent({ type: 'keyUp', keyCode: 'D', modifiers });
    await sleep(60);
  };
  window.show(); window.focus();
  await manager.present({ epoch: target.epoch, revision: 999, blocked: false, slots: [{ tabId: target.tabId, slotId: target.tabId, bounds: { x: 20, y: 70, width: 1000, height: 650 } }] });
  guest.focus(); await sleep(100);
  assert.ok(guest.isFocused());
  await guest.executeJavaScript('globalThis.designKeyLeaks = 0; addEventListener("keydown", e => { if(e.key.toLowerCase() === "d") designKeyLeaks++; })');
  await key(guest);
  assert.equal(toggleCount(), 1);
  assert.equal(await guest.executeJavaScript('designKeyLeaks'), 0);
  for (const modifiers of [['meta'], ['shift'], ['control', 'shift'], ['meta', 'shift', 'alt'], ['meta', 'shift', 'control']]) await key(guest, modifiers);
  assert.equal(toggleCount(), 1);
  await key(guest, ['meta', 'shift', 'isAutoRepeat']);
  assert.equal(toggleCount(), 1);
  pass('Cmd+Shift+D native guest focus routes once; repeats/extra/missing modifiers rejected, no guest key leakage');
  const shortcutState = await design.start(target);
  await design.intent({ revision: revision(shortcutState), intent: { kind: 'pick', x: 180, y: 140 } });
  const shortcutOverlay = (window.contentView.children.at(-1) as WebContentsView).webContents;
  await sleep(150);
  shortcutOverlay.focus();
  await shortcutOverlay.executeJavaScript('document.querySelector("textarea").focus()');
  await key(shortcutOverlay);
  assert.equal(toggleCount(), 2);
  await key(shortcutOverlay, ['meta', 'shift', 'isAutoRepeat']);
  await key(shortcutOverlay, ['meta', 'shift', 'alt']);
  assert.equal(toggleCount(), 2);
  window.webContents.focus();
  await key(shortcutOverlay);
  await key(guest);
  assert.equal(toggleCount(), 2);
  const releaseShortcutBlock = manager.blockNative();
  await key(shortcutOverlay);
  await key(guest);
  assert.equal(toggleCount(), 2);
  releaseShortcutBlock();
  await manager.present({ epoch: target.epoch, revision: 1000, blocked: false, slots: [{ tabId: target.tabId, slotId: target.tabId, bounds: { x: 20, y: 70, width: 1000, height: 650 } }] });
  shortcutOverlay.focus();
  assert.equal(manager.designShortcut({ ...target, generation: 'stale' }, { type: 'keyDown', key: 'D', meta: true, shift: true } as Electron.Input), false);
  const overlayClosed = once(shortcutOverlay, 'destroyed');
  await design.stop(lease(shortcutState));
  await overlayClosed;
  assert.ok(shortcutOverlay.isDestroyed());
  assert.equal(toggleCount(), 2);
  pass('Cmd+Shift+D trusted overlay composer routes; native security hiding and stale tab identities reject');
}
async function run() {
  await app.whenReady();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  window = new BrowserWindow({
    width: 1100,
    height: 820,
    show: true,
    webPreferences: {
      preload: path.join(directory, "preload.cjs"),
      additionalArguments: ["--whip-browser-tabs"],
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  await window.loadURL(origin + "/app");
  const shortcuts: BrowserEvent[] = [];
  manager = new BrowserManager(window, event => { shortcuts.push(event); }, {
    presentDesign: (id, bounds) => design?.present(id, bounds),
    invalidateControl: (id, reason) => design?.invalidate(id, reason),
  });
  design = new BrowserDesignController(window, manager, {
    url: origin + "/design.html",
    preload: path.join(directory, "browser-design-preload.cjs"),
    emit: (event) => {
      events.push(event);
      window.webContents.send("whip:browser-design:event", event);
    },
  });
  cleanup = installBrowserDesignIPC(window, design, (event) => {
    if (event.senderFrame?.url !== origin + "/app")
      throw new Error("Untrusted app URL");
  });
  const epoch = manager.snapshot().epoch;
  const tab = manager.create({
    epoch,
    url: origin + "/page?token=URL_SECRET#fragment",
  });
  const target = { epoch, tabId: tab.id, generation: tab.generation };
  await manager.admitted(target);
  await manager.present({
    epoch,
    revision: 1,
    blocked: false,
    slots: [
      {
        tabId: tab.id,
        slotId: tab.id,
        bounds: { x: 20, y: 70, width: 1000, height: 650 },
      },
    ],
  });
  const guest = await manager.controlledContents(target);
  if (guest.isLoading()) await once(guest, "did-stop-loading");
  if (process.env.BROWSER_DESIGN_SHORTCUT_ONLY === '1') {
    await shortcutChecks(window, manager, design, target, guest, shortcuts);
    await manager.close(target);
    return;
  }
  const startupCommand = guest.debugger.sendCommand.bind(guest.debugger);
  for (const failure of [
    "DOM.enable",
    "Page.getFrameTree",
    "Page.createIsolatedWorld",
    "Runtime.addBinding",
    "Runtime.evaluate",
  ]) {
    guest.debugger.sendCommand = async (method, params, session) => {
      if (method === failure) throw new Error("fixture startup failure");
      return startupCommand(method, params, session);
    };
    await assert.rejects(design.start(target), /fixture startup failure/);
    assert.equal(guest.debugger.isAttached(), false);
    assert.equal(design.model(), undefined);
  }
  guest.debugger.sendCommand = async (method, params, session) => {
    if (method === "DOM.enable") {
      const detached = once(guest.debugger, "detach");
      guest.debugger.detach();
      await detached;
      guest.debugger.attach("1.3");
      throw new Error("fixture ownership replaced");
    }
    return startupCommand(method, params, session);
  };
  await assert.rejects(design.start(target), /fixture ownership replaced/);
  assert.equal(guest.debugger.isAttached(), true);
  guest.debugger.detach();
  guest.debugger.sendCommand = startupCommand;
  pass(
    "each startup CDP failure releases acquired debugger; loss and replacement during startup does not detach another owner",
  );
  guest.debugger.sendCommand = async (method, params, session) =>
    method === "DOM.enable"
      ? new Promise(() => {})
      : startupCommand(method, params, session);
  await assert.rejects(design.start(target), /timed out/);
  assert.equal(guest.debugger.isAttached(), false);
  guest.debugger.sendCommand = async (method, params, session) => {
    if (method === "DOM.enable")
      await guest.loadURL(origin + "/page?token=URL_SECRET&navigation=1");
    return startupCommand(method, params, session);
  };
  await assert.rejects(design.start(target), /page changed/);
  assert.equal(guest.debugger.isAttached(), false);
  const closing = new BrowserDesignController(window, manager, {
    url: origin + "/design.html",
    preload: path.join(directory, "browser-design-preload.cjs"),
    emit: () => {},
  });
  guest.debugger.sendCommand = async (method, params, session) => {
    if (method === "DOM.enable") closing.dispose();
    return startupCommand(method, params, session);
  };
  await assert.rejects(closing.start(target));
  assert.equal(guest.debugger.isAttached(), false);
  guest.debugger.sendCommand = startupCommand;
  pass(
    "startup timeout, concurrent document navigation, and dispose release the lease and never publish a stale overlay",
  );
  let state = (await window.webContents.executeJavaScript(
    `whipDesktop.browser.design.start(${JSON.stringify(target)})`,
  )) as BrowserDesignState;
  const overlay = window.contentView.children.at(-1) as WebContentsView;
  const overlayContents = overlay.webContents;
  assert.equal(
    await guest.executeJavaScript(
      "[typeof whipDesktop,typeof whipBrowserDesign].join()",
    ),
    "undefined,undefined",
  );
  assert.equal(
    await overlay.webContents.executeJavaScript("typeof whipDesktop"),
    "undefined",
  );
  await sleep(300);
  console.log(
    "OVERLAY_RENDER",
    await overlay.webContents.executeJavaScript("document.body.innerText"),
  );
  assert.ok(
    await overlay.webContents.executeJavaScript(
      'document.body.innerText.includes("Select an element")',
    ),
  );
  if (process.platform === "darwin") {
    const file = path.join(directory, "production-composited.png");
    execFileSync("/usr/sbin/screencapture", [
      "-x",
      "-o",
      "-l",
      window.getMediaSourceId().split(":")[1],
      file,
    ]);
    const screenshot = nativeImage.createFromPath(file),
      pixels = screenshot.toBitmap(),
      scale = screenshot.getSize().width / window.getBounds().width,
      offset =
        (Math.floor(
          (80 + window.getContentBounds().y - window.getBounds().y) * scale,
        ) *
          screenshot.getSize().width +
          Math.floor(30 * scale)) *
        4;
    const pixel = [...pixels.subarray(offset, offset + 4)];
    assert.ok(
      pixel.every(
        (value, index) => Math.abs(value - [240, 232, 223, 255][index]) <= 3,
      ),
      JSON.stringify(pixel),
    );
    pass(
      "OS-composited production themed React overlay remains transparent above live guest at device scale " +
        scale,
    );
  }
  const act = async (intent: unknown) => {
    await overlay.webContents.executeJavaScript(
      `whipBrowserDesign.intent(${JSON.stringify({ revision: revision(state), intent })})`,
    );
    state = design.model()!.state;
  };
  await act({ kind: "hover", x: 120, y: 90 });
  assert.match(design.model()!.state.hover!.label, /Alpha/);
  const firstHover = state.hover!;
  const firstGeometry = state.hoverGeometryRevision;
  assert.match(firstHover.id, /^[a-f0-9-]{36}$/);
  await act({ kind: "hover", x: 125, y: 95 });
  assert.equal(state.hover!.id, firstHover.id);
  await act({ kind: "hover", x: 340, y: 90 });
  assert.notEqual(state.hover!.id, firstHover.id);
  assert.equal(state.hoverGeometryRevision, firstGeometry);
  const secondHover = state.hover!;
  await guest.executeJavaScript(
    "document.querySelector('#second').style.left='310px'",
  );
  await sleep(550);
  state = design.model()!.state;
  assert.equal(state.hover!.id, secondHover.id);
  assert.equal(state.hover!.bounds.x, 310);
  assert.ok(state.hoverGeometryRevision! > firstGeometry!);
  await guest.executeJavaScript(
    "document.querySelector('#second').style.left='300px'",
  );
  await sleep(350);
  await act({ kind: "hover", x: 120, y: 90 });
  const beforeScroll = state.hoverGeometryRevision!;
  await guest.executeJavaScript("scrollTo(0,20)");
  await sleep(80);
  state = design.model()!.state;
  assert.equal(state.hover, undefined);
  assert.ok(state.hoverGeometryRevision! > beforeScroll);
  await guest.executeJavaScript("scrollTo(0,0)");
  await sleep(80);
  await act({ kind: "hover", x: 120, y: 90 });
  const beforeResize = state.hoverGeometryRevision!;
  const originalBounds = overlay.getBounds();
  design.present(state.tabId, {
    ...originalBounds,
    width: originalBounds.width - 1,
  });
  assert.equal(design.model()!.state.hover, undefined);
  assert.ok(design.model()!.state.hoverGeometryRevision! > beforeResize);
  design.present(state.tabId, originalBounds);
  await act({ kind: "hover", x: 120, y: 90 });
  const beforeZoom = state.hoverGeometryRevision!;
  guest.setZoomFactor(1.25);
  await sleep(100);
  state = design.model()!.state;
  assert.equal(state.hover, undefined);
  assert.ok(state.hoverGeometryRevision! > beforeZoom);
  guest.setZoomFactor(1);
  await sleep(100);
  assert.equal(
    await guest.executeJavaScript("typeof __whipDesignGeometry"),
    "undefined",
  );
  pass(
    "opaque hover identity is stable only for the same live target; layout motion preserves identity and bumps geometry revision; page scroll, resize and zoom clear/snap; geometry binding isolated from guest",
  );
  await act({ kind: "hover", x: 120, y: 90 });
  const beforeDetachedHover = state.hoverGeometryRevision!;
  const detachedHoverId = state.hover!.id;
  await guest.executeJavaScript(
    "window.detachedFirst=document.querySelector('#first');detachedFirst.remove()",
  );
  await sleep(550);
  state = design.model()!.state;
  assert.equal(state.hover, undefined);
  assert.ok(state.hoverGeometryRevision! > beforeDetachedHover);
  await act({ kind: "hover", x: 340, y: 90 });
  assert.notEqual(state.hover!.id, detachedHoverId);
  assert.ok(state.hoverGeometryRevision! > beforeDetachedHover);
  await guest.executeJavaScript("document.body.prepend(detachedFirst)");
  pass(
    "detached hover clears target and increments geometry revision before replacement, including coalesced renderer snapshots",
  );
  // Hold a target switch across a scroll. Its old result must never repopulate hover.
  {
    const send = guest.debugger.sendCommand.bind(guest.debugger);
    let release!: () => void, began!: () => void;
    const ready = new Promise<void>((resolve) => {
      began = resolve;
    });
    let held = false;
    guest.debugger.sendCommand = async (method, params, session) => {
      if (method === "DOM.resolveNode" && !held) {
        held = true;
        began();
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      }
      return send(method, params, session);
    };
    const pendingHover = design.intent({
      revision: revision(state),
      intent: { kind: "hover", x: 340, y: 90 },
    });
    await ready;
    await design.intent({
      revision: revision(state),
      intent: { kind: "scroll", x: 340, y: 90, deltaX: 0, deltaY: 1 },
    });
    release();
    await pendingHover;
    guest.debugger.sendCommand = send;
    assert.equal(design.model()!.state.hover, undefined);
    await guest.executeJavaScript("scrollTo(0,0)");
    await sleep(100);
  }
  pass(
    "scroll invalidates in-flight target switches before they can restore stale hover geometry",
  );
  await act({ kind: "hover", x: 120, y: 90 });
  await overlayContents.executeJavaScript(
    "new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))",
  );
  await act({ kind: "hover", x: 340, y: 90 });
  const motion =
    await overlayContents.executeJavaScript(`new Promise(resolve => requestAnimationFrame(() => {
    const box = document.querySelector('[data-design-hover]');
    const animations = box?.getAnimations() ?? [];
    for (const animation of animations) { animation.pause(); animation.currentTime = 25; }
    resolve({ duration: box && getComputedStyle(box).transitionDuration, active: animations.length });
  }))`);
  assert.equal(motion.duration, "0.1s");
  assert.ok(
    motion.active > 0,
    "target switch must animate in actual trusted native renderer",
  );
  const beforeMotionPick = state.selectionRevision;
  overlayContents.sendInputEvent({
    type: "mouseDown",
    x: 340,
    y: 90,
    button: "left",
    clickCount: 1,
  });
  overlayContents.sendInputEvent({
    type: "mouseUp",
    x: 340,
    y: 90,
    button: "left",
    clickCount: 1,
  });
  for (
    let attempt = 0;
    attempt < 20 &&
    design.model()!.state.selectionRevision === beforeMotionPick;
    attempt++
  )
    await sleep(10);
  state = design.model()!.state;
  assert.equal(state.elements.length, 1);
  assert.match(state.elements[0].label, /Beta/);
  assert.equal(state.hover, undefined);
  assert.equal(await guest.executeJavaScript("window.clicks"), 0);
  pass(
    "actual native overlay animates target switch for 100ms; click during paused interpolation immediately selects real pointer target, not displayed bounds, without guest input",
  );
  await act({ kind: "clear" });
  await act({ kind: "hover", x: 120, y: 90 });
  await act({ kind: "pick", x: 120, y: 90 });
  assert.equal(state.elements.length, 1);
  const firstRevision = revision(state);
  await act({ kind: "pick", x: 340, y: 90, additive: true });
  assert.equal(state.elements.length, 2);
  assert.notEqual(state.elements[0].color, state.elements[1].color);
  assert.equal(await guest.executeJavaScript("window.clicks"), 0);
  assert.equal(
    await overlay.webContents.executeJavaScript(
      "typeof window.whipBrowserDesign.intent",
    ),
    "function",
  );
  pass(
    "actual built React overlay + dedicated preload + strict IPC + native isolated-world two-element selection/hover; guest has no bridge and no click effects",
  );
  assert.equal(
    await overlay.webContents.executeJavaScript(
      `whipBrowserDesign.intent(${JSON.stringify({ revision: firstRevision, intent: { kind: "clear" } })}).then(()=>false,()=>true)`,
    ),
    true,
  );
  assert.equal(
    await window.webContents.executeJavaScript(
      `whipDesktop.browser.design.capture(${JSON.stringify({ ...firstRevision, screenshot: false })}).then(()=>false,()=>true)`,
    ),
    true,
  );
  pass("stale selection revisions rejected across real IPC");
  const original = guest.debugger.sendCommand.bind(guest.debugger);
  let releaseObservation!: () => void, started!: () => void;
  let delayed = false;
  const began = new Promise<void>((resolve) => {
    started = resolve;
  });
  guest.debugger.sendCommand = async (method, params, session) => {
    if (method === "DOM.resolveNode" && !delayed) {
      delayed = true;
      started();
      await new Promise<void>((resolve) => {
        releaseObservation = resolve;
      });
    }
    return original(method, params, session);
  };
  const hover = design.intent({
    revision: revision(state),
    intent: { kind: "hover", x: 120, y: 90 },
  });
  await began;
  const duringHover = design.intent({
    revision: revision(state),
    intent: { kind: "pick", x: 340, y: 90 },
  });
  releaseObservation();
  await Promise.all([hover, duringHover]);
  guest.debugger.sendCommand = original;
  state = design.model()!.state;
  assert.equal(state.elements.length, 1);
  assert.match(state.elements[0].label, /Beta/);
  const same = revision(state);
  const clicks = await Promise.allSettled([
    design.intent({
      revision: same,
      intent: { kind: "pick", x: 120, y: 90, additive: true },
    }),
    design.intent({
      revision: same,
      intent: { kind: "pick", x: 340, y: 90, additive: true },
    }),
  ]);
  assert.equal(clicks[0].status, "fulfilled");
  assert.equal(clicks[1].status, "rejected");
  state = design.model()!.state;
  assert.equal(state.elements.length, 2);
  pass(
    "a click during deferred hover selects once; concurrent discrete picks serialize and stale second request rejects rather than disappearing",
  );
  await act({ kind: "clear" });
  await act({ kind: "next" });
  assert.match(state.hover!.label, /Alpha/);
  await act({ kind: "pick-hover" });
  assert.match(state.elements[0].label, /Alpha/);
  await guest.executeJavaScript(
    "document.querySelector('#second').style.top='1000px'",
  );
  await act({ kind: "next" });
  assert.match(state.hover!.label, /Beta/);
  assert.ok(await guest.executeJavaScript("scrollY > 0"));
  await sleep(80);
  state = design.model()!.state;
  assert.match(state.hover!.label, /Beta/);
  await act({ kind: "pick-hover", additive: true });
  assert.equal(state.elements.length, 2);
  await act({ kind: "pick-hover", additive: true });
  assert.equal(state.elements.length, 1);
  await guest.executeJavaScript(
    "document.querySelector('#second').style.top='60px';scrollTo(0,0)",
  );
  await sleep(100);
  await guest.executeJavaScript(`(() => {
    const beta=document.querySelector('#second'), container=document.createElement('div');
    container.id='nested-scroll'; container.style.cssText='position:absolute;left:300px;top:60px;width:180px;height:100px;overflow:auto';
    beta.before(container); container.append(beta); beta.style.cssText='position:relative;left:0;top:500px';
  })()`);
  await act({ kind: "previous" });
  await act({ kind: "next" });
  await sleep(80);
  state = design.model()!.state;
  assert.match(state.hover!.label, /Beta/);
  assert.ok(
    await guest.executeJavaScript(
      "document.querySelector('#nested-scroll').scrollTop > 0",
    ),
  );
  await act({ kind: "pick-hover", additive: true });
  assert.equal(state.elements.length, 2);
  await act({ kind: "pick-hover", additive: true });
  await guest.executeJavaScript(`(() => {
    const beta=document.querySelector('#second'), container=document.querySelector('#nested-scroll');
    container.replaceWith(beta); beta.style.cssText='position:absolute;left:300px;top:60px';scrollTo(0,0);
  })()`);
  await sleep(100);
  await act({ kind: "hover", x: 340, y: 90 });
  pass(
    "keyboard traversal reveals offscreen and nested-scroll targets, retains hover through following frames, and Enter selects the revealed target",
  );
  await act({ kind: "pick", x: 340, y: 90, additive: true });
  await act({ kind: "pick-hover", additive: true });
  assert.equal(state.elements.length, 1);
  await act({ kind: "pick-hover", additive: true });
  assert.equal(state.elements.length, 2);
  pass(
    "bounded semantic keyboard target traversal, additive pick-hover/toggle work without page input or focus grants",
  );
  // Reveal completion must not mask a native hide/show invalidation (ABA).
  {
    const send = guest.debugger.sendCommand.bind(guest.debugger);
    let release!: () => void, began!: () => void;
    const ready = new Promise<void>((resolve) => {
      began = resolve;
    });
    let held = false;
    guest.debugger.sendCommand = async (method, params, session) => {
      const result = await send(method, params, session);
      if (method === "Runtime.evaluate" && params?.awaitPromise && !held) {
        held = true;
        began();
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      }
      return result;
    };
    const reveal = design.intent({
      revision: revision(state),
      intent: { kind: "next" },
    });
    await ready;
    const bounds = overlay.getBounds();
    design.present(undefined);
    design.present(state.tabId, bounds);
    release();
    await reveal;
    guest.debugger.sendCommand = send;
    await sleep(80);
    state = design.model()!.state;
    assert.equal(state.hover, undefined);
  }
  pass(
    "native hide/show during awaited keyboard reveal invalidates stale completion even after presentation returns",
  );
  const draft = {
    prompt: "Change both",
    recipients: [
      { id: "host:root:root", label: "Test conversation", available: true },
      { id: "host:root:other", label: "Other conversation", available: true },
    ],
    recipientId: "host:root:root",
    screenshot: true,
    delivery: "queue",
    busy: false,
    uncertain: false,
    theme: { name: "default", mode: "light" },
  };
  await window.webContents.executeJavaScript(
    `whipDesktop.browser.design.update(${JSON.stringify({ ...lease(state), draft })})`,
  );
  await act({ kind: "prompt", value: "Make both clearer" });
  await act({ kind: "recipient", id: "host:root:root" });
  await act({ kind: "send" });
  assert.deepEqual(
    events
      .filter((event) => event.kind === "intent")
      .map((event) => event.intent.kind),
    ["prompt", "recipient", "send"],
  );
  const capture = await window.webContents.executeJavaScript(
    `whipDesktop.browser.design.capture(${JSON.stringify({ ...revision(state), screenshot: true })})`,
  );
  assert.ok(capture.image.startsWith("data:image/png;base64,"));
  assert.ok(capture.image.length <= 12 * 1024 * 1024);
  assert.ok(Buffer.byteLength(capture.text) <= 65536);
  assert.equal(JSON.parse(capture.text).elements.length, 2);
  assert.ok(!capture.text.includes("URL_SECRET"));
  pass(
    "actual root app preload/update and overlay prompt/recipient/send intents; bounded two-element JSON + PNG capture with URL secret removed",
  );
  const overlayClick = async (expression: string) => {
    const point = await overlayContents.executeJavaScript(`(() => {
      const element = ${expression};
      if (!element) throw Error("Missing overlay control: " + ${JSON.stringify(expression)});
      const rect = element.getBoundingClientRect();
      return { x: Math.round(rect.x + rect.width / 2), y: Math.round(rect.y + rect.height / 2) };
    })()`);
    overlayContents.sendInputEvent({ type: "mouseMove", ...point });
    overlayContents.sendInputEvent({
      type: "mouseDown",
      button: "left",
      clickCount: 1,
      ...point,
    });
    overlayContents.sendInputEvent({
      type: "mouseUp",
      button: "left",
      clickCount: 1,
      ...point,
    });
    await sleep(180);
  };
  const conversation =
    'document.querySelector(\'[role="combobox"][aria-label="Conversation"]\')';
  const portalOpen = () =>
    overlayContents.executeJavaScript(
      '!!document.querySelector(\'[role="combobox"][aria-expanded="true"]\')',
    );
  const selectedBeforePortal = revision(design.model()!.state);
  await overlayClick(conversation);
  assert.equal(await portalOpen(), true);
  assert.equal(
    await overlayContents.executeJavaScript(
      'document.querySelector(\'[role="listbox"]\').closest("form") === null',
    ),
    true,
  );
  assert.equal(overlay.getVisible(), true);
  assert.equal(guest.isDestroyed(), false);
  await overlayClick(
    '[...document.querySelectorAll(\'[role="option"]\')].find(item => item.textContent.includes("Other conversation"))',
  );
  assert.ok(
    events.some(
      (event) =>
        event.kind === "intent" &&
        event.intent.kind === "recipient" &&
        event.intent.id === "host:root:other",
    ),
  );
  assert.equal(await portalOpen(), false);
  await overlayClick(
    'document.querySelector(\'[role="combobox"][aria-label="Delivery when busy"]\')',
  );
  await overlayClick(
    '[...document.querySelectorAll(\'[role="option"]\')].find(item => item.textContent.includes("Steer"))',
  );
  assert.ok(
    events.some(
      (event) =>
        event.kind === "intent" &&
        event.intent.kind === "delivery" &&
        event.intent.value === "steer",
    ),
  );
  await overlayClick(conversation);
  assert.equal(await portalOpen(), true);
  overlayContents.sendInputEvent({ type: "keyDown", keyCode: "ESC" });
  overlayContents.sendInputEvent({ type: "keyUp", keyCode: "ESC" });
  await sleep(180);
  assert.equal(await portalOpen(), false);
  assert.deepEqual(revision(design.model()!.state), selectedBeforePortal);
  assert.equal(await guest.executeJavaScript("window.clicks"), 0);
  pass(
    "shared Conversation/Delivery portals accept native mouse input inside trusted overlay; no guest clicks or selection leak; Escape closes only dropdown",
  );
  guest.setZoomFactor(1.5);
  await sleep(150);
  await act({ kind: "pick", x: 180, y: 135 });
  const zoomCapture = await design.capture({
    ...revision(state),
    screenshot: true,
  });
  const geometry = JSON.parse(zoomCapture.text);
  const picture = nativeImage.createFromDataURL(zoomCapture.image!);
  assert.equal(geometry.coordinateSpace, "viewport-css-pixels");
  assert.equal(geometry.zoomFactor, 1.5);
  assert.equal(geometry.screenshot.width, picture.getSize().width);
  assert.ok(Math.abs(geometry.elements[0].bounds.x - 80) < 1);
  assert.equal(
    geometry.screenshot.cssToImageX,
    geometry.screenshot.width / geometry.viewport.width,
  );
  assert.ok(geometry.viewport.devicePixelRatio >= 1.5);
  guest.setZoomFactor(1);
  await sleep(100);
  pass(
    "150% guest zoom capture declares CSS coordinates, DPR, actual resized image dimensions and CSS-to-image transform",
  );
  const captureCommand = guest.debugger.sendCommand.bind(guest.debugger);
  guest.debugger.sendCommand = async (method, params, session) => {
    const result = await captureCommand(method, params, session);
    if (method === "Page.captureScreenshot")
      await guest.executeJavaScript(
        'document.getElementById("first").textContent="Changed during capture"',
      );
    return result;
  };
  await assert.rejects(
    design.capture({ ...revision(state), screenshot: true }),
    /changed during capture/,
  );
  guest.debugger.sendCommand = captureCommand;
  await guest.executeJavaScript(
    'document.getElementById("first").textContent="Alpha"',
  );
  pass(
    "same-document selected content mutation after screenshot is rejected before evidence release",
  );
  await act({ kind: "pick", x: 100, y: 260 });
  const privacy = await design.capture({
    ...revision(state),
    screenshot: false,
  });
  assert.match(privacy.text, /Visible evidence/);
  for (const secret of [
    "CSS_HIDDEN_SECRET",
    "HIDDEN_SECRET",
    "PASSWORD_SECRET",
    "TEXTAREA_SECRET",
    "EDITABLE_SECRET",
  ])
    assert.ok(!privacy.text.includes(secret), secret);
  pass(
    "isolated-world evidence ignores hostile main-world prototype override, hidden CSS ancestors and input/textarea/editable values",
  );
  await act({ kind: "pick", x: 120, y: 90 });
  await guest.executeJavaScript('document.getElementById("first").remove()');
  await sleep(500);
  state = design.model()!.state;
  assert.equal(state.status, "stale");
  await assert.rejects(
    design.capture({ ...revision(state), screenshot: false }),
    /stale/,
  );
  await act({ kind: "clear" });
  await act({ kind: "pick", x: 340, y: 90 });
  assert.equal(state.status, "active");
  pass(
    "live hot-reload/detached-node becomes stale and cannot capture; explicit reselect recovers",
  );
  await manager.present({
    epoch,
    revision: 2,
    blocked: true,
    slots: [
      {
        tabId: tab.id,
        slotId: tab.id,
        bounds: { x: 20, y: 70, width: 1000, height: 650 },
      },
    ],
  });
  assert.ok(window.contentView.children.every((view) => !view.getVisible()));
  await manager.present({
    epoch,
    revision: 3,
    blocked: false,
    slots: [
      {
        tabId: tab.id,
        slotId: tab.id,
        bounds: { x: 20, y: 70, width: 1000, height: 650 },
      },
    ],
  });
  assert.equal(window.contentView.children.at(-1), overlay);
  assert.equal(overlay.getVisible(), true);
  await overlayClick(conversation);
  assert.equal(await portalOpen(), true);
  const release = manager.blockNative();
  assert.equal(overlay.getVisible(), false);
  release();
  assert.equal(overlay.getVisible(), false);
  pass(
    "production manager hide ACK and native security block synchronously hide both surfaces including open dropdown portal; unblocking alone cannot restore",
  );
  const attacker = new BrowserWindow({
    show: false,
    webPreferences: {
      preload: path.join(directory, "browser-design-preload.cjs"),
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  await attacker.loadURL(origin + "/design.html");
  assert.equal(
    await attacker.webContents.executeJavaScript(
      "whipBrowserDesign.snapshot().then(()=>false,()=>true)",
    ),
    true,
  );
  attacker.destroy();
  pass(
    "foreign renderer with same overlay URL/preload cannot invoke scoped Design IPC",
  );
  const old = lease(state);
  await guest.loadURL(origin + "/other");
  assert.equal(design.model(), undefined);
  assert.equal(guest.debugger.isAttached(), false);
  assert.ok(overlayContents.isDestroyed());
  await assert.rejects(
    design.capture({
      ...old,
      documentRevision: state.documentRevision,
      selectionRevision: state.selectionRevision,
      screenshot: false,
    }),
    /ended/,
  );
  pass(
    "navigation tears down overlay/debugger, stale lease rejected; guest remains human-owned",
  );
  await shortcutChecks(window, manager, design, target, guest, shortcuts);
  await manager.close(target);
  assert.ok(guest.isDestroyed());
}
run()
  .then(() => console.log("DESIGN_PRODUCTION_NATIVE_OK"))
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  })
  .finally(() => {
    cleanup?.();
    design?.dispose();
    manager?.dispose();
    if (window && !window.isDestroyed()) window.destroy();
    server.close();
    app.exit(process.exitCode ?? 0);
  });
