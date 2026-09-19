import {
  app,
  BrowserWindow,
  WebContentsView,
  ipcMain,
  nativeImage,
  type WebContents,
  type IpcMainInvokeEvent,
} from "electron";
import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { once } from "node:events";
import { writeFileSync } from "node:fs";
import { BrowserManager } from "../src/browser-manager";

const directory = process.env.BROWSER_DESIGN_DIRECTORY!;
app.setPath("userData", path.join(directory, "profile"));
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
const pass = (name: string, evidence?: unknown) =>
  console.log("PASS", name, evidence ?? "");
let window: BrowserWindow | undefined,
  manager: BrowserManager | undefined,
  overlay: WebContentsView | undefined;
let guest: WebContents | undefined;
let picks = 0;
const bounds = { x: 30, y: 60, width: 800, height: 580 };
const server = http.createServer((request, response) => {
  response.setHeader("Content-Type", "text/html");
  if (request.url === "/overlay") {
    response.setHeader(
      "Content-Security-Policy",
      "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'",
    );
    response.end(`<!doctype html><style>
      /* Geometry/colors are test probes, not product styling. */
      html,body { margin:0; width:100%; height:100%; background:transparent; overflow:hidden }
      #outline { position:absolute; border:3px solid #168aff; pointer-events:none; display:none }
      #composer { position:absolute; left:330px; top:180px; width:390px; padding:12px; background:rgb(230,30,190); border-radius:16px; font:14px sans-serif }
      textarea { display:block; width:360px; height:70px; resize:none }
    </style><div id=outline></div><section id=composer aria-label="Describe change"><label for=description>Describe change</label><textarea id=description></textarea><button>Send</button></section>
    <script>
      window.lastPick = null; window.pickError = '';
      document.addEventListener('pointerdown', async event => {
        if (event.target.closest('#composer')) return;
        event.preventDefault(); event.stopImmediatePropagation();
        try {
          const selected = await designFixture.pick({x:event.clientX,y:event.clientY});
          window.lastPick=selected;
          Object.assign(document.querySelector('#outline').style,{display:'block',left:selected.rect.x+'px',top:selected.rect.y+'px',width:selected.rect.width+'px',height:selected.rect.height+'px'});
        } catch(error) {window.pickError=String(error)}
      },true);
      document.addEventListener('wheel', event => { if(event.target.closest('#composer')) return; event.preventDefault(); void designFixture.scroll({x:event.clientX,y:event.clientY,deltaY:event.deltaY}); },{passive:false});
    </script>`);
  } else
    response.end(
      `<!doctype html><title>Untrusted guest</title><style>body {margin:0;background:rgb(40,180,90);height:2400px} #target {position:absolute;left:80px;top:80px;width:160px;height:80px} #input {position:absolute;left:80px;top:280px}</style><button id=target>Change me</button><input id=input value="page value"><script>window.activations=0;document.querySelector('#target').onclick=()=>window.activations++;window.addEventListener('keydown',()=>window.guestKeys=(window.guestKeys||0)+1);</script>`,
    );
});
function trusted(event: IpcMainInvokeEvent) {
  if (
    !overlay ||
    event.sender !== overlay.webContents ||
    event.senderFrame !== overlay.webContents.mainFrame
  )
    throw new Error("Untrusted Design sender");
}
function point(value: unknown): { x: number; y: number; deltaY?: number } {
  assert.ok(value && typeof value === "object");
  const p = value as { x: number; y: number; deltaY?: number };
  assert.ok(
    Number.isFinite(p.x) &&
      Number.isFinite(p.y) &&
      p.x >= 0 &&
      p.y >= 0 &&
      p.x < bounds.width &&
      p.y < bounds.height,
  );
  if (p.deltaY !== undefined)
    assert.ok(Number.isFinite(p.deltaY) && Math.abs(p.deltaY) <= 1000);
  return p;
}
async function click(contents: WebContents, x: number, y: number) {
  contents.sendInputEvent({
    type: "mouseDown",
    x,
    y,
    button: "left",
    clickCount: 1,
  });
  contents.sendInputEvent({
    type: "mouseUp",
    x,
    y,
    button: "left",
    clickCount: 1,
  });
  await sleep(120);
}
async function run() {
  await app.whenReady();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  window = new BrowserWindow({
    width: 900,
    height: 740,
    show: true,
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  await window.loadURL(
    'data:text/html,<body style="background:white">Trusted app fixture',
  );
  manager = new BrowserManager(window, () => {});
  const epoch = manager.snapshot().epoch;
  const tab = manager.create({ epoch, url: origin + "/page" });
  const target = { epoch, tabId: tab.id, generation: tab.generation };
  await manager.admitted(target);
  let revision = 0;
  const present = (blocked = false) =>
    manager!.present({
      epoch,
      revision: ++revision,
      blocked,
      slots: [{ tabId: tab.id, slotId: tab.id, bounds }],
    });
  await present();
  guest = await manager.controlledContents(target);
  if (guest.isLoading()) await once(guest, "did-stop-loading");
  assert.equal(
    await guest.executeJavaScript("typeof designFixture"),
    "undefined",
  );
  guest.debugger.attach("1.3");
  await guest.debugger.sendCommand("DOM.enable");
  await guest.debugger.sendCommand("Runtime.enable");
  ipcMain.handle("design-fixture:pick", async (event, value) => {
    trusted(event);
    const p = point(value);
    const hit = await guest!.debugger.sendCommand("DOM.getNodeForLocation", {
      x: Math.round(p.x / guest!.getZoomFactor()),
      y: Math.round(p.y / guest!.getZoomFactor()),
      includeUserAgentShadowDOM: false,
    });
    const resolved = await guest!.debugger.sendCommand("DOM.resolveNode", {
      backendNodeId: hit.backendNodeId,
    });
    const observation = await guest!.debugger.sendCommand(
      "Runtime.callFunctionOn",
      {
        objectId: resolved.object.objectId,
        functionDeclaration:
          'function(){const r=this.getBoundingClientRect();return {id:this.id,tag:this.localName,text:(this.innerText||"").slice(0,160),rect:{x:r.x,y:r.y,width:r.width,height:r.height}}}',
        returnByValue: true,
      },
    );
    await guest!.debugger.sendCommand("Runtime.releaseObject", {
      objectId: resolved.object.objectId,
    });
    picks++;
    return observation.result.value;
  });
  ipcMain.handle("design-fixture:scroll", async (event, value) => {
    trusted(event);
    const p = point(value);
    guest!.sendInputEvent({
      type: "mouseWheel",
      x: p.x,
      y: p.y,
      deltaY: -(p.deltaY ?? 0),
      deltaX: 0,
      canScroll: true,
    });
  });
  overlay = new WebContentsView({
    webPreferences: {
      preload: path.join(directory, "browser-design-native-preload.cjs"),
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: false,
    },
  });
  overlay.setBackgroundColor("#00000000");
  overlay.setBounds(bounds);
  window.contentView.addChildView(overlay);
  await overlay.webContents.loadURL(origin + "/overlay");
  app.focus({ steal: true });
  window.focus();
  overlay.webContents.focus();
  await sleep(300);
  assert.equal(window.contentView.children.at(-1), overlay);
  assert.ok(window.contentView.children.every((view) => view.getVisible()));
  const initial = await window.capturePage();
  writeFileSync(path.join(directory, "window.png"), initial.toPNG());
  const mediaId = window.getMediaSourceId().split(":")[1];
  execFileSync("/usr/sbin/screencapture", [
    "-x",
    "-o",
    "-l",
    mediaId,
    path.join(directory, "composited.png"),
  ]);
  const composited = nativeImage.createFromPath(
    path.join(directory, "composited.png"),
  );
  const bitmap = composited.toBitmap(),
    scale = composited.getSize().width / window.getBounds().width;
  const contentOffset = window.getContentBounds().y - window.getBounds().y;
  const pixel = (x: number, y: number) => {
    const offset =
      (Math.floor((y + contentOffset) * scale) * composited.getSize().width +
        Math.floor(x * scale)) *
      4;
    return [...bitmap.subarray(offset, offset + 4)];
  };
  const guestPixel = pixel(bounds.x + 20, bounds.y + 20),
    composerPixel = pixel(bounds.x + 335, bounds.y + 230);
  assert.ok(
    guestPixel.every(
      (value, index) => Math.abs(value - [90, 180, 40, 255][index]) <= 3,
    ),
    JSON.stringify(guestPixel),
  );
  assert.ok(
    composerPixel.every(
      (value, index) => Math.abs(value - [190, 30, 230, 255][index]) <= 3,
    ),
    JSON.stringify(composerPixel),
  );
  pass(
    "composited window pixels prove live guest under transparent overlay and opaque composer above it",
    { guestPixel, composerPixel, deviceScale: scale },
  );
  const overlayImage = await overlay.webContents.capturePage();
  writeFileSync(path.join(directory, "overlay.png"), overlayImage.toPNG());
  pass(
    "transparent sibling created above simultaneously visible production guest",
    {
      children: window.contentView.children.length,
      overlay: overlay.getBounds(),
      windowImage: initial.getSize(),
    },
  );
  const contentBounds = window.getContentBounds();
  execFileSync(path.join(directory, "native-input"), [
    String(contentBounds.x + bounds.x + 120),
    String(contentBounds.y + bounds.y + 110),
  ]);
  await sleep(200);
  const selected = await overlay.webContents.executeJavaScript(
    "({picked:window.lastPick,error:window.pickError})",
  );
  console.log("OS_INPUT_PROBE", {
    selected,
    activations: await guest.executeJavaScript("window.activations"),
    contentBounds,
    focused: window.isFocused(),
  });
  assert.equal(selected.error, "");
  assert.equal(selected.picked?.id, "target");
  assert.equal(await guest.executeJavaScript("window.activations"), 0);
  pass(
    "native trusted pointer input hits underlying DOM through main; selected link/button never activated",
    selected.picked,
  );
  await click(overlay.webContents, 410, 220);
  await overlay.webContents.insertText("Make this clear 日本語");
  assert.equal(
    await overlay.webContents.executeJavaScript(
      'document.querySelector("textarea").value',
    ),
    "Make this clear 日本語",
  );
  assert.equal(
    await guest.executeJavaScript('document.querySelector("input").value'),
    "page value",
  );
  assert.equal(await guest.executeJavaScript("window.guestKeys||0"), 0);
  assert.equal(overlay.webContents.isFocused(), true);
  pass(
    "floating composer owns focus and committed Unicode text; guest not edited (hardware IME composition still manual)",
  );
  const before = await guest.debugger.sendCommand("Page.captureScreenshot", {
    format: "png",
  });
  await overlay.webContents.executeJavaScript(
    'document.querySelector("#composer").style.background="blue"',
  );
  const after = await guest.debugger.sendCommand("Page.captureScreenshot", {
    format: "png",
  });
  assert.equal(before.data, after.data);
  writeFileSync(
    path.join(directory, "guest.png"),
    Buffer.from(before.data, "base64"),
  );
  pass(
    "guest CDP screenshot excludes composer and all trusted overlay pixels",
    { bytes: Buffer.from(before.data, "base64").length },
  );
  await overlay.webContents.executeJavaScript(
    "designFixture.scroll({x:120,y:110,deltaY:300})",
  );
  await sleep(250);
  const scrolled = await guest.executeJavaScript("scrollY");
  assert.ok(scrolled > 0);
  pass("only explicit bounded wheel action reaches guest scroll", {
    scrollY: scrolled,
  });
  await guest.executeJavaScript("scrollTo(0,0)");
  await sleep(100);
  guest.setZoomFactor(1.5);
  await sleep(150);
  await click(overlay.webContents, 180, 165);
  assert.equal(
    await overlay.webContents.executeJavaScript("window.lastPick.id"),
    "target",
  );
  pass(
    "page zoom 150% converts overlay DIP pointer to page CSS hit coordinates",
  );
  // Feasibility composition of existing ACK with new owned surface. Production must make this atomic inside manager presentation.
  overlay.setVisible(false);
  await present(true);
  assert.ok(window.contentView.children.every((view) => !view.getVisible()));
  assert.equal(window.webContents.isFocused(), true);
  pass(
    "existing production blocked presentation ACK is preserved when Design surface hides before awaiting it",
  );
  await present(false);
  overlay.setVisible(true);
  assert.ok(window.contentView.children.every((view) => view.getVisible()));
  assert.throws(
    () => guest!.debugger.attach("1.3"),
    /already attached|attached/i,
  );
  pass(
    "exclusive debugger ownership fails busy rather than stealing another lease",
  );
  window.contentView.removeChildView(overlay);
  overlay.webContents.close();
  overlay = undefined;
  guest.debugger.detach();
  await manager.close(target);
  assert.ok(guest.isDestroyed());
  assert.equal(window.contentView.children.length, 0);
  pass(
    "teardown removes surface, closes renderer, detaches debugger and closes guest",
    { picks },
  );
}
run()
  .then(() => console.log("DESIGN_NATIVE_SPIKE_OK"))
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  })
  .finally(() => {
    ipcMain.removeHandler("design-fixture:pick");
    ipcMain.removeHandler("design-fixture:scroll");
    if (overlay && !overlay.webContents.isDestroyed())
      overlay.webContents.close();
    manager?.dispose();
    if (window && !window.isDestroyed()) window.destroy();
    server.close();
    app.exit(process.exitCode ?? 0);
  });
