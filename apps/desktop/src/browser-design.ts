import {
  WebContentsView,
  nativeImage,
  type BrowserWindow,
  type WebContents,
  type IpcMainInvokeEvent,
  type Rectangle,
  type Input,
  type Event,
} from "electron";
import { randomUUID } from "node:crypto";
import type {
  BrowserDesignCapture,
  BrowserDesignDraft,
  BrowserDesignElement,
  BrowserDesignEvent,
  BrowserDesignLease,
  BrowserDesignModel,
  BrowserDesignRevision,
  BrowserDesignState,
} from "@whip/app/desktop-bridge";
import { BrowserManager } from "./browser-manager";
import { object, target } from "./browser-policy";
import {
  designBoolean,
  designDraft,
  designIntent,
  designLease,
  designLimits,
  designRevision,
  evidenceURL,
} from "./browser-design-policy";
import { readDesignElement } from "./browser-design-page";

type Observation = {
  tag: string;
  role: string;
  name: string;
  text: string;
  attributes: Record<string, string>;
  styles: Record<string, string>;
  selector: string;
  frame?: string;
  bounds: Rectangle;
  unsupported?: string;
};
type Selected = { backendNodeId: number; element: BrowserDesignElement };
type Active = {
  state: BrowserDesignState;
  draft: BrowserDesignDraft;
  guest: WebContents;
  view: WebContentsView;
  frameId: string;
  contextId: number;
  nodes: Selected[];
  visible: boolean;
  timer?: ReturnType<typeof setInterval>;
  cleanup: () => void;
  ownsDebugger: () => boolean;
  sequence: number;
  pending: boolean;
  lastIntent: number;
  intents: number;
  keyboardIndex: number;
  hoverNode?: number;
  zoom: number;
};
const initialDraft = (): BrowserDesignDraft => ({
  prompt: "",
  recipients: [],
  recipientId: "",
  screenshot: true,
  delivery: "queue",
  busy: false,
  uncertain: false,
  theme: { name: "default", mode: "dark" },
});
const colors = ["blue", "purple", "green", "orange"] as const;

/** Exclusive human inspection. No browser-agent grants or arbitrary renderer CDP. */
export class BrowserDesignController {
  private active?: Active;
  private starting = false;
  private disposed = false;
  private selectionQueue: Promise<void> = Promise.resolve();
  private selectionPending = 0;
  constructor(
    private window: BrowserWindow,
    private manager: BrowserManager,
    private options: {
      url: string;
      preload: string;
      emit: (event: BrowserDesignEvent) => void;
    },
  ) {}
  model(): BrowserDesignModel | undefined {
    const a = this.active;
    return a ? { state: a.state, draft: a.draft } : undefined;
  }
  assertOverlay(event: IpcMainInvokeEvent): void {
    const a = this.active;
    if (
      !a ||
      event.sender !== a.view.webContents ||
      event.senderFrame !== a.view.webContents.mainFrame ||
      event.senderFrame.url !== this.options.url
    )
      throw new Error("Untrusted Design overlay");
  }
  private current(
    lease: BrowserDesignLease,
    revision?: BrowserDesignRevision,
  ): Active {
    const a = this.active;
    if (
      !a ||
      this.disposed ||
      a.state.designId !== lease.designId ||
      a.state.epoch !== lease.epoch ||
      a.state.tabId !== lease.tabId ||
      a.state.generation !== lease.generation
    )
      throw new Error("Design session ended");
    const live = this.manager.controlledState(lease);
    if (
      live.documentGeneration !== a.state.documentRevision ||
      a.guest.isDestroyed()
    )
      throw new Error("The page changed. Select the elements again.");
    if (
      revision &&
      (revision.documentRevision !== a.state.documentRevision ||
        revision.selectionRevision !== a.state.selectionRevision)
    )
      throw new Error("Design selection changed. Try again.");
    return a;
  }
  private publish(a: Active, state = true): void {
    if (this.active !== a) return;
    if (!a.view.webContents.isDestroyed())
      a.view.webContents.send("whip:browser-design-overlay:model", {
        state: a.state,
        draft: a.draft,
      });
    if (state) this.options.emit({ kind: "state", state: a.state });
  }
  async start(value: unknown): Promise<BrowserDesignState> {
    const requested = target(value);
    if (this.disposed || this.starting) throw new Error("Design is busy");
    if (this.active) {
      if (this.active.state.tabId === requested.tabId)
        return this.current({
          ...requested,
          designId: this.active.state.designId,
        }).state;
      this.end("stopped");
    }
    this.starting = true;
    let view: WebContentsView | undefined,
      guest: WebContents | undefined,
      attached = false;
    let cleanup = () => {};
    try {
      guest = await this.manager.controlledContents(requested);
      if (this.disposed) throw new Error("Design is closed");
      if (guest.debugger.isAttached() || guest.isDevToolsOpened())
        throw new Error(
          "Browser is busy. Close DevTools or end agent browser inspection before entering Design.",
        );
      const initial = this.manager.controlledState(requested);
      const lost = () => {
        attached = false;
        if (this.active?.guest === guest)
          this.end(
            "stopped",
            "Design ended because the page or inspector became unavailable.",
          );
      };
      guest.debugger.attach("1.3");
      attached = true;
      guest.debugger.on("detach", lost);
      guest.on("destroyed", lost);
      cleanup = () => {
        guest!.debugger.removeListener("detach", lost);
        guest!.removeListener("destroyed", lost);
      };
      const startup = async (
        method: string,
        params: Record<string, unknown> = {},
      ) => {
        if (!attached) throw new Error("Design inspector ownership ended");
        const result = await this.boundedCommand(guest!, method, params);
        if (
          this.disposed ||
          !attached ||
          this.manager.controlledState(requested).documentGeneration !==
            initial.documentGeneration
        )
          throw new Error("The page changed while starting Design. Try again.");
        return result;
      };
      await startup("DOM.enable");
      const tree = await startup("Page.getFrameTree");
      const frameId = tree.frameTree.frame.id as string;
      const world = await startup("Page.createIsolatedWorld", {
        frameId,
        worldName: "whip-design-observation",
      });
      const live = this.manager.controlledState(requested);
      if (this.disposed) throw new Error("Design is closed");
      view = new WebContentsView({
        webPreferences: {
          preload: this.options.preload,
          sandbox: true,
          contextIsolation: true,
          nodeIntegration: false,
          webviewTag: false,
          webSecurity: true,
          spellcheck: true,
          navigateOnDragDrop: false,
        },
      });
      view.setBackgroundColor("#00000000");
      view.setVisible(false);
      view.webContents.setZoomFactor(1);
      view.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
      view.webContents.on("will-navigate", (event) => event.preventDefault());
      view.webContents.on("will-redirect", (event) => event.preventDefault());
      view.webContents.on("will-attach-webview", (event) =>
        event.preventDefault(),
      );
      const state: BrowserDesignState = {
        ...requested,
        designId: randomUUID(),
        documentRevision: live.documentGeneration,
        selectionRevision: 0,
        hoverGeometryRevision: 0,
        status: "active",
        viewport: { width: 0, height: 0 },
        elements: [],
      };
      const a: Active = {
        state,
        draft: initialDraft(),
        guest,
        view,
        frameId,
        contextId: world.executionContextId,
        nodes: [],
        visible: false,
        cleanup: () => {},
        ownsDebugger: () => attached,
        sequence: 0,
        pending: false,
        lastIntent: Date.now(),
        intents: 0,
        keyboardIndex: -1,
        zoom: guest.getZoomFactor(),
      };
      const overlayLost = () => {
        if (this.active === a)
          this.end("stopped", "Design overlay became unavailable.");
      };
      const shortcut = (event: Event, input: Input) => {
        if (this.active === a && a.visible && view!.webContents.isFocused() &&
          this.manager.designShortcut(a.state, input)) event.preventDefault();
      };
      view.webContents.on('before-input-event', shortcut);
      // Only this main-created isolated world can issue geometry invalidations.
      const geometryBinding = "__whipDesignGeometry";
      const geometry = (
        _event: unknown,
        method: string,
        params: Record<string, unknown>,
      ) => {
        if (
          method === "Runtime.bindingCalled" &&
          params.name === geometryBinding &&
          params.executionContextId === a.contextId &&
          params.payload === ""
        )
          this.invalidateHover(a);
      };
      guest.debugger.on("message", geometry);
      a.cleanup = () => {
        cleanup();
        view!.webContents.removeListener('before-input-event', shortcut);
        guest!.debugger.removeListener("message", geometry);
        view!.webContents.removeListener("render-process-gone", overlayLost);
      };
      view.webContents.on("render-process-gone", overlayLost);
      this.active = a;
      await startup("Runtime.addBinding", {
        name: geometryBinding,
        executionContextId: a.contextId,
      });
      await startup("Runtime.evaluate", {
        contextId: a.contextId,
        expression: `(() => {
          globalThis.__whipDesignGeometryCleanup?.();
          const controller = new AbortController();
          const changed = () => globalThis.${geometryBinding}('');
          const options = {capture:true, passive:true, signal:controller.signal};
          addEventListener('scroll', changed, options);
          addEventListener('resize', changed, options);
          visualViewport?.addEventListener('scroll', changed, options);
          visualViewport?.addEventListener('resize', changed, options);
          globalThis.__whipDesignGeometryCleanup = () => controller.abort();
        })()`,
      });
      this.window.contentView.addChildView(view);
      await this.deadline(view.webContents.loadURL(this.options.url), 10000);
      this.current(state);
      a.timer = setInterval(() => {
        void this.refresh(a);
      }, 250);
      a.timer.unref();
      this.manager.refreshPresentation();
      if (a.visible) view.webContents.focus();
      this.publish(a);
      return a.state;
    } catch (error) {
      if (view && this.active?.view === view) this.end("stopped");
      else {
        cleanup();
        if (view && !view.webContents.isDestroyed()) view.webContents.close();
        if (
          attached &&
          guest &&
          !guest.isDestroyed() &&
          guest.debugger.isAttached()
        )
          guest.debugger.detach();
      }
      throw error;
    } finally {
      this.starting = false;
    }
  }
  stop(value: unknown): void {
    const lease = designLease(value);
    if (!this.active) return;
    this.current(lease);
    this.end("stopped");
  }
  update(value: unknown): void {
    const input = object(value, [
      "epoch",
      "tabId",
      "generation",
      "designId",
      "draft",
    ]);
    const a = this.current(designLease(input, ["draft"]));
    a.draft = designDraft(input.draft);
    this.publish(a, false);
  }
  /** Called synchronously by BrowserManager.hide/present, before native hide acknowledgement. */
  present(tabId?: string, bounds?: Rectangle): void {
    const a = this.active;
    if (!a) return;
    if (!tabId) {
      if (a.visible) {
        ++a.sequence;
        this.invalidateHover(a);
      }
      a.visible = false;
      a.view.setVisible(false);
      return;
    }
    if (tabId !== a.state.tabId || !bounds) return;
    if (
      a.state.viewport.width !== bounds.width ||
      a.state.viewport.height !== bounds.height ||
      a.zoom !== a.guest.getZoomFactor()
    )
      this.invalidateHover(a);
    a.view.setBounds(bounds);
    a.visible = true;
    this.window.contentView.addChildView(a.view);
    a.view.setVisible(true);
    if (
      a.state.viewport.width !== bounds.width ||
      a.state.viewport.height !== bounds.height
    ) {
      a.state = {
        ...a.state,
        viewport: { width: bounds.width, height: bounds.height },
      };
      this.publish(a);
    }
  }
  private invalidateHover(a: Active): void {
    if (this.active !== a) return;
    a.zoom = a.guest.getZoomFactor();
    a.hoverNode = undefined;
    a.state = {
      ...a.state,
      hover: undefined,
      hoverGeometryRevision: (a.state.hoverGeometryRevision ?? 0) + 1,
    };
    this.publish(a);
  }
  invalidate(tabId: string, reason: string): void {
    if (this.active?.state.tabId === tabId)
      this.end(
        "stopped",
        reason === "document-changed"
          ? "The page changed. Your description is preserved; enter Design to select again."
          : "Design ended: " + reason,
      );
  }
  private end(status: BrowserDesignState["status"], error?: string): void {
    const a = this.active;
    if (!a) return;
    this.active = undefined;
    clearInterval(a.timer);
    a.sequence++;
    a.cleanup();
    a.view.setVisible(false);
    if (!this.window.isDestroyed())
      this.window.contentView.removeChildView(a.view);
    if (!a.view.webContents.isDestroyed()) a.view.webContents.close();
    if (
      a.ownsDebugger() &&
      !a.guest.isDestroyed() &&
      a.guest.debugger.isAttached()
    )
      a.guest.debugger.detach();
    this.options.emit({
      kind: "state",
      state: { ...a.state, status, ...(error ? { error } : {}) },
    });
    if (!this.window.isDestroyed() && !this.window.webContents.isDestroyed())
      this.window.webContents.focus();
  }
  dispose(): void {
    this.disposed = true;
    this.end("stopped");
  }
  private async command(
    a: Active,
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<any> {
    this.current(a.state);
    const result = await this.boundedCommand(a.guest, method, params);
    this.current(a.state);
    return result;
  }
  private async boundedCommand(
    guest: WebContents,
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<any> {
    return this.deadline(guest.debugger.sendCommand(method, params), 2000);
  }
  private async deadline<T>(
    work: Promise<T>,
    milliseconds: number,
  ): Promise<T> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        work,
        new Promise<never>((_, reject) => {
          timer = setTimeout(
            () => reject(new Error("Design observation timed out")),
            milliseconds,
          );
        }),
      ]);
    } finally {
      clearTimeout(timer);
    }
  }
  private async observe(
    a: Active,
    backendNodeId: number,
  ): Promise<Observation> {
    const resolved = await this.command(a, "DOM.resolveNode", {
      backendNodeId,
      executionContextId: a.contextId,
    });
    try {
      const result = await this.command(a, "Runtime.callFunctionOn", {
        objectId: resolved.object.objectId,
        functionDeclaration: readDesignElement,
        returnByValue: true,
      });
      const observation = result.result?.value as Observation | undefined;
      if (!observation || result.exceptionDetails)
        throw new Error(
          "Selected element is no longer available. Remove it and select again.",
        );
      if (observation.unsupported) throw new Error(observation.unsupported);
      if (JSON.stringify(observation).length > 8192)
        throw new Error("Element context exceeds its limit");
      if (!Object.values(observation.bounds).every(Number.isFinite))
        throw new Error("Invalid element geometry");
      return observation;
    } finally {
      if (this.active === a)
        await this.command(a, "Runtime.releaseObject", {
          objectId: resolved.object.objectId,
        }).catch(() => {});
    }
  }
  private element(
    a: Active,
    id: string,
    observation: Observation,
    index: number,
  ): BrowserDesignElement {
    const zoom = a.guest.getZoomFactor(),
      r = observation.bounds;
    return {
      id,
      label: (
        observation.role +
        (observation.frame
          ? " · frame container"
          : observation.name
            ? " · " + observation.name
            : "")
      ).slice(0, 180),
      number: index + 1,
      color: colors[index % colors.length],
      bounds: {
        x: r.x * zoom,
        y: r.y * zoom,
        width: r.width * zoom,
        height: r.height * zoom,
      },
    };
  }
  private selected(a: Active): void {
    a.state = {
      ...a.state,
      status: "active",
      error: undefined,
      selectionRevision: a.state.selectionRevision + 1,
      elements: a.nodes.map((node) => node.element),
      hover: undefined,
    };
    this.publish(a);
  }
  intent(value: unknown): Promise<void> {
    const input = object(value, ["revision", "intent"]),
      intent = designIntent(input.intent);
    if (
      !["pick", "ancestor", "pick-hover", "next", "previous"].includes(
        intent.kind,
      )
    )
      return this.applyIntent(value);
    if (this.selectionPending >= 8)
      return Promise.reject(new Error("Selection is updating. Try again."));
    this.selectionPending++;
    const work = this.selectionQueue
      .then(() => this.applyIntent(value))
      .finally(() => {
        this.selectionPending--;
      });
    this.selectionQueue = work.catch(() => {});
    return work;
  }
  private async applyIntent(value: unknown): Promise<void> {
    const input = object(value, ["revision", "intent"]),
      revision = designRevision(input.revision),
      intent = designIntent(input.intent);
    const a = this.current(revision, revision);
    if (a.zoom !== a.guest.getZoomFactor()) this.invalidateHover(a);
    if (Date.now() - a.lastIntent > 1000) {
      a.lastIntent = Date.now();
      a.intents = 0;
    }
    if (++a.intents > 120) throw new Error("Design input rate exceeded");
    if (intent.kind === "stop") {
      this.end("stopped");
      return;
    }
    if (
      [
        "prompt",
        "recipient",
        "screenshot",
        "delivery",
        "capture",
        "send",
        "evidence-close",
      ].includes(intent.kind)
    ) {
      this.options.emit({ kind: "intent", revision, intent });
      return;
    }
    if (a.state.status === "capturing" || a.draft.busy || a.draft.uncertain) {
      if (intent.kind === "hover") return;
      throw new Error(
        "Design is sending or awaiting confirmation. Wait before changing the selection.",
      );
    }
    if (intent.kind === "clear") {
      a.nodes = [];
      this.selected(a);
      return;
    }
    if (intent.kind === "remove") {
      a.nodes = a.nodes.filter((node) => node.element.id !== intent.id);
      this.selected(a);
      return;
    }
    if (intent.kind === "scroll") {
      if (
        !a.visible ||
        intent.x < 0 ||
        intent.y < 0 ||
        intent.x >= a.state.viewport.width ||
        intent.y >= a.state.viewport.height
      )
        return;
      this.invalidateHover(a);
      a.guest.sendInputEvent({
        type: "mouseWheel",
        x: Math.round(intent.x),
        y: Math.round(intent.y),
        deltaX: -intent.deltaX,
        deltaY: -intent.deltaY,
        canScroll: true,
      });
      return;
    }
    // Never drop a click merely because a hover or geometry refresh is in flight.
    // A newer discrete pick supersedes observational work through this sequence.
    if ((a.pending || this.selectionPending > 0) && intent.kind === "hover")
      return;
    a.pending = true;
    const sequence = ++a.sequence;
    let hoverGeometryRevision = a.state.hoverGeometryRevision;
    try {
      let backendNodeId: number;
      if (intent.kind === "pick-hover") {
        if (!a.hoverNode) throw new Error("Move to an element first.");
        backendNodeId = a.hoverNode;
      } else if (intent.kind === "next" || intent.kind === "previous") {
        if (!a.visible) return;
        a.keyboardIndex += intent.kind === "next" ? 1 : -1;
        const found = await this.command(a, "Runtime.evaluate", {
          contextId: a.contextId,
          expression: `(async()=>{const items=[];const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_ELEMENT);let node,count=0;while((node=walker.nextNode())&&count++<2000&&items.length<200){if(node.matches('button,a[href],input:not([type=hidden]),select,textarea,[role],h1,h2,h3,p,section,main,article')&&node.checkVisibility({checkOpacity:true,checkVisibilityCSS:true}))items.push(node)}if(!items.length)return null;const index=((${a.keyboardIndex}%items.length)+items.length)%items.length;const chosen=items[index];chosen.scrollIntoView({block:'nearest',inline:'nearest',behavior:'instant'});await new Promise(requestAnimationFrame);return chosen})()`,
          awaitPromise: true,
          returnByValue: false,
        });
        // The traversal's own reveal scroll is complete; later geometry still invalidates it.
        hoverGeometryRevision = a.state.hoverGeometryRevision;
        if (!found.result?.objectId)
          throw new Error(
            "No visible semantic elements available for keyboard selection.",
          );
        try {
          const described = await this.command(a, "DOM.describeNode", {
            objectId: found.result.objectId,
          });
          backendNodeId = described.node.backendNodeId;
        } finally {
          await this.command(a, "Runtime.releaseObject", {
            objectId: found.result.objectId,
          });
        }
      } else if (intent.kind === "ancestor") {
        const node = a.nodes.at(-1);
        if (!node) return;
        const resolved = await this.command(a, "DOM.resolveNode", {
          backendNodeId: node.backendNodeId,
          executionContextId: a.contextId,
        });
        try {
          const parent = await this.command(a, "Runtime.callFunctionOn", {
            objectId: resolved.object.objectId,
            functionDeclaration: "function(){return this.parentElement}",
            returnByValue: false,
          });
          if (!parent.result.objectId) return;
          try {
            const described = await this.command(a, "DOM.describeNode", {
              objectId: parent.result.objectId,
            });
            backendNodeId = described.node.backendNodeId;
          } finally {
            await this.command(a, "Runtime.releaseObject", {
              objectId: parent.result.objectId,
            });
          }
        } finally {
          await this.command(a, "Runtime.releaseObject", {
            objectId: resolved.object.objectId,
          });
        }
      } else {
        if (intent.kind !== "hover" && intent.kind !== "pick") return;
        if (
          !a.visible ||
          intent.x < 0 ||
          intent.y < 0 ||
          intent.x >= a.state.viewport.width ||
          intent.y >= a.state.viewport.height
        )
          return;
        const hit = await this.command(a, "DOM.getNodeForLocation", {
          x: Math.round(intent.x / a.guest.getZoomFactor()),
          y: Math.round(intent.y / a.guest.getZoomFactor()),
          includeUserAgentShadowDOM: false,
        });
        backendNodeId = hit.backendNodeId;
        if (hit.frameId && hit.frameId !== a.frameId) {
          const owner = await this.command(a, "DOM.getFrameOwner", {
            frameId: hit.frameId,
          });
          backendNodeId = owner.backendNodeId;
        }
      }
      const observed = await this.observe(a, backendNodeId);
      this.current(revision, revision);
      if (a.sequence !== sequence) return;
      const existing = a.nodes.find(
        (node) => node.backendNodeId === backendNodeId,
      );
      if (
        intent.kind === "hover" ||
        intent.kind === "next" ||
        intent.kind === "previous"
      ) {
        if (
          !a.visible ||
          a.state.hoverGeometryRevision !== hoverGeometryRevision
        )
          return;
        const id =
          a.hoverNode === backendNodeId && a.state.hover
            ? a.state.hover.id
            : randomUUID();
        a.hoverNode = backendNodeId;
        const hover = this.element(a, id, observed, 0);
        if (JSON.stringify(hover) !== JSON.stringify(a.state.hover)) {
          a.state = {
            ...a.state,
            hover,
            hoverGeometryRevision:
              (a.state.hoverGeometryRevision ?? 0) +
              (a.state.hover?.id === hover.id &&
              JSON.stringify(a.state.hover.bounds) !==
                JSON.stringify(hover.bounds)
                ? 1
                : 0),
          };
          this.publish(a);
        }
        return;
      }
      if (
        (intent.kind === "pick" || intent.kind === "pick-hover") &&
        intent.additive &&
        existing
      )
        a.nodes = a.nodes.filter((node) => node !== existing);
      else {
        if (
          (intent.kind === "pick" && !intent.additive) ||
          (intent.kind === "pick-hover" && !intent.additive)
        )
          a.nodes = [];
        if (intent.kind === "ancestor") a.nodes.pop();
        if (!a.nodes.some((node) => node.backendNodeId === backendNodeId)) {
          if (a.nodes.length >= designLimits.selections)
            throw new Error("Select at most 16 elements.");
          a.nodes.push({
            backendNodeId,
            element:
              existing?.element ??
              this.element(
                a,
                randomUUID(),
                observed,
                Math.max(0, ...a.nodes.map((node) => node.element.number)),
              ),
          });
        }
      }
      this.selected(a);
    } catch (error) {
      if (this.active === a && intent.kind !== "hover") {
        a.state = {
          ...a.state,
          error:
            error instanceof Error ? error.message : "Could not select element",
        };
        this.publish(a);
      }
    } finally {
      a.pending = false;
    }
  }
  private async refresh(a: Active): Promise<void> {
    if (
      this.active !== a ||
      !a.visible ||
      a.pending ||
      this.selectionPending ||
      a.state.status === "capturing" ||
      (!a.nodes.length && !a.state.hover)
    )
      return;
    if (a.zoom !== a.guest.getZoomFactor()) this.invalidateHover(a);
    a.pending = true;
    const revision = a.state.selectionRevision;
    const sequence = a.sequence;
    const hoverGeometryRevision = a.state.hoverGeometryRevision;
    try {
      const nodes: Selected[] = [];
      for (const node of a.nodes) {
        const observed = await this.observe(a, node.backendNodeId);
        nodes.push({
          ...node,
          element: {
            ...this.element(
              a,
              node.element.id,
              observed,
              node.element.number - 1,
            ),
            color: node.element.color,
          },
        });
      }
      let hover = a.state.hover;
      if (hover && a.hoverNode) {
        // Re-observe the same node: layout motion must snap, never look like a target switch.
        try {
          hover = this.element(
            a,
            hover.id,
            await this.observe(a, a.hoverNode),
            0,
          );
        } catch {
          hover = undefined;
        }
      }
      if (
        this.active !== a ||
        a.state.selectionRevision !== revision ||
        a.sequence !== sequence ||
        a.state.hoverGeometryRevision !== hoverGeometryRevision
      )
        return;
      if (
        JSON.stringify(nodes.map((node) => node.element)) !==
          JSON.stringify(a.state.elements) ||
        JSON.stringify(hover) !== JSON.stringify(a.state.hover)
      ) {
        a.nodes = nodes;
        if (!hover) a.hoverNode = undefined;
        a.state = {
          ...a.state,
          hover,
          elements: nodes.map((node) => node.element),
          hoverGeometryRevision:
            (a.state.hoverGeometryRevision ?? 0) +
            (JSON.stringify(a.state.hover?.bounds) !==
            JSON.stringify(hover?.bounds)
              ? 1
              : 0),
        };
        this.publish(a);
      }
    } catch (error) {
      if (
        this.active === a &&
        a.state.selectionRevision === revision &&
        a.state.status !== "stale"
      ) {
        a.state = {
          ...a.state,
          status: "stale",
          error: error instanceof Error ? error.message : "Selection changed",
        };
        this.publish(a);
      }
    } finally {
      a.pending = false;
    }
  }
  async capture(value: unknown): Promise<BrowserDesignCapture> {
    const input = object(value, [
      "epoch",
      "tabId",
      "generation",
      "designId",
      "documentRevision",
      "selectionRevision",
      "screenshot",
    ]);
    const revision = designRevision(input, ["screenshot"]),
      screenshot = designBoolean(input.screenshot),
      a = this.current(revision, revision);
    if (this.selectionPending)
      throw new Error("Selection is updating. Try again.");
    if (a.state.status === "capturing" || !a.nodes.length)
      throw new Error("Select elements before capturing context.");
    if (a.state.status === "stale")
      throw new Error(
        "Selection is stale. Remove missing elements and select again.",
      );
    a.sequence++;
    a.state = { ...a.state, status: "capturing" };
    this.publish(a);
    try {
      const elements = [];
      for (const node of a.nodes) {
        elements.push({
          number: node.element.number,
          ...(await this.observe(a, node.backendNodeId)),
        });
        this.current(revision, revision);
      }
      const live = this.manager.controlledState(a.state),
        capturedAt = new Date().toISOString();
      const metrics = await this.command(a, "Runtime.evaluate", {
        contextId: a.contextId,
        expression: "({width:innerWidth,height:innerHeight,devicePixelRatio})",
        returnByValue: true,
      });
      const viewport = metrics.result.value as {
        width: number;
        height: number;
        devicePixelRatio: number;
      };
      if (
        !Object.values(viewport).every(
          (value) => Number.isFinite(value) && value > 0,
        )
      )
        throw new Error("Invalid viewport geometry");
      const zoomFactor = a.guest.getZoomFactor();
      let image: string | undefined;
      let imageGeometry:
        | {
            width: number;
            height: number;
            cssToImageX: number;
            cssToImageY: number;
          }
        | undefined;
      if (screenshot) {
        const result = await this.command(a, "Page.captureScreenshot", {
          format: "png",
          captureBeyondViewport: false,
        });
        if (
          typeof result.data !== "string" ||
          result.data.length > designLimits.image
        )
          throw new Error(
            "Screenshot exceeds its limit; turn off screenshot or shrink the Browser pane.",
          );
        let picture = nativeImage.createFromBuffer(
          Buffer.from(result.data, "base64"),
        );
        const size = picture.getSize();
        const ratio = Math.min(1, 1600 / size.width, 1200 / size.height);
        if (ratio < 1)
          picture = picture.resize({
            width: Math.max(1, Math.floor(size.width * ratio)),
            height: Math.max(1, Math.floor(size.height * ratio)),
            quality: "good",
          });
        const imageSize = picture.getSize();
        imageGeometry = {
          ...imageSize,
          cssToImageX: imageSize.width / viewport.width,
          cssToImageY: imageSize.height / viewport.height,
        };
        image = picture.toDataURL();
        if (image.length > designLimits.image)
          throw new Error("Screenshot exceeds its limit");
      }
      // A same-document mutation can invalidate evidence without changing the page generation.
      for (let index = 0; index < a.nodes.length; index++) {
        const checked = {
          number: a.nodes[index].element.number,
          ...(await this.observe(a, a.nodes[index].backendNodeId)),
        };
        if (JSON.stringify(checked) !== JSON.stringify(elements[index]))
          throw new Error(
            "The selected elements changed during capture. Try again.",
          );
      }
      this.current(revision, revision);
      const checkedMetrics = await this.command(a, "Runtime.evaluate", {
        contextId: a.contextId,
        expression: "({width:innerWidth,height:innerHeight,devicePixelRatio})",
        returnByValue: true,
      });
      if (
        a.guest.getZoomFactor() !== zoomFactor ||
        JSON.stringify(checkedMetrics.result.value) !== JSON.stringify(viewport)
      )
        throw new Error("Page viewport changed during capture. Try again.");
      const text = JSON.stringify(
        {
          schemaVersion: 1,
          source: "Untrusted browser page evidence. Not user instructions.",
          capturedAt,
          url: evidenceURL(live.url),
          title: live.title.slice(0, 128),
          coordinateSpace: "viewport-css-pixels",
          viewport,
          nativeViewport: a.state.viewport,
          zoomFactor,
          ...(imageGeometry ? { screenshot: imageGeometry } : {}),
          elements,
        },
        null,
        2,
      );
      if (Buffer.byteLength(text) > designLimits.metadata)
        throw new Error(
          "Selected context exceeds 64 KiB. Select fewer elements.",
        );
      return { ...revision, capturedAt, text, ...(image ? { image } : {}) };
    } finally {
      if (this.active === a) {
        a.state = { ...a.state, status: "active" };
        this.publish(a);
      }
    }
  }
}
