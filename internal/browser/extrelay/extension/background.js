// The toolbar pin owns one exact tab/socket pair. A lost connection ends that
// selection; neither the worker nor the host reconnects or resends effects.
const SESSION_ID = "whip-ext";
const MAX_MESSAGE_BYTES = 12 * 1024 * 1024;
const MAX_PENDING_CALLS = 32;
let selected = null;
let selecting = false;
let retiring = null;
let pendingCalls = 0;
let autoAttaching = false;

async function readRelay() {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 3000);
  try {
    const response = await fetch(chrome.runtime.getURL("relay.json"), { signal: controller.signal });
    if (!response.ok) throw new Error("Start the native extension relay before pinning a tab");
    const text = await response.text();
    if (text.length > 4096) throw new Error("Relay configuration exceeds bounds");
    const value = JSON.parse(text);
    if (typeof value.addr !== "string" || !/^127\.0\.0\.1:[1-9][0-9]{0,4}$/.test(value.addr) || Number(value.addr.split(":")[1]) > 65535 || typeof value.token !== "string" || !/^[a-f0-9]{48}$/.test(value.token)) {
      throw new Error("Invalid native relay configuration");
    }
    return value;
  } finally {
    clearTimeout(timer);
  }
}

async function swlog(step, extra) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 3000);
  try {
    const { addr, token } = await readRelay();
    await fetch(`http://${addr}/swlog?token=${token}`, {
      method: "POST", signal: controller.signal,
      body: (step + (extra === undefined ? "" : `: ${extra}`)).slice(0, 1024),
    });
  } catch (_) {
    // Diagnostics never select a tab or retry an effect.
  } finally {
    clearTimeout(timer);
  }
}

function current(binding) {
  return selected === binding && !binding.closed;
}

function badge(binding, on) {
  chrome.action.setBadgeText({ text: on ? "●" : "", tabId: binding.tabId }).catch(() => {});
  if (on) chrome.action.setBadgeBackgroundColor({ color: "#16a34a", tabId: binding.tabId }).catch(() => {});
}

function send(binding, value) {
  if (!current(binding) || binding.socket?.readyState !== WebSocket.OPEN) return;
  const encoded = JSON.stringify(value);
  if (encoded.length > MAX_MESSAGE_BYTES || new TextEncoder().encode(encoded).length > MAX_MESSAGE_BYTES || binding.socket.bufferedAmount + encoded.length > MAX_MESSAGE_BYTES) {
    void unpin(binding);
    return;
  }
  binding.socket.send(encoded);
}

// Local revocation is synchronous. The browser API's in-flight commands cannot
// be undone; their late results stay with the retired binding and are discarded.
function unpin(binding = selected) {
  if (!binding || binding.closed) return retiring ?? Promise.resolve();
  binding.closed = true;
  if (selected === binding) selected = null;
  const socket = binding.socket;
  if (socket) {
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null;
    socket.close();
  }
  badge(binding, false);
  const cleanup = (async () => {
    if (binding.attached) { try { await chrome.debugger.detach({ tabId: binding.tabId }); } catch (_) {} }
  })();
  retiring = cleanup;
  cleanup.finally(() => { if (retiring === cleanup) retiring = null; });
  return cleanup;
}

async function pin(tabId) {
  if (selected || retiring || !Number.isSafeInteger(tabId) || tabId < 0) throw new Error("Browser selection is unavailable");
  const binding = { tabId, socket: null, closed: false, attached: false, requests: new Set() };
  selected = binding;
  try {
    const { addr, token } = await readRelay();
    if (!current(binding)) return;
    await chrome.debugger.attach({ tabId }, "1.3");
    binding.attached = true;
    if (!current(binding)) { try { await chrome.debugger.detach({ tabId }); } catch (_) {} return; }
    const socket = new WebSocket(`ws://${addr}/ext?token=${token}`);
    binding.socket = socket;
    socket.onopen = async () => {
      let title = "", url = "";
      try {
        const tab = await chrome.tabs.get(tabId);
        title = typeof tab.title === "string" ? tab.title : "";
        url = typeof tab.url === "string" ? tab.url : "";
      } catch (_) {}
      if (!current(binding)) return;
      if (new TextEncoder().encode(title).length > 4096 || new TextEncoder().encode(url).length > 8192) { await unpin(binding); return; }
      send(binding, { method: "whip.attached", params: { tabId, title, url } });
      if (current(binding)) badge(binding, true);
    };
    socket.onmessage = (event) => {
      if (!current(binding)) return;
      try {
        if (typeof event.data !== "string" || event.data.length > MAX_MESSAGE_BYTES || new TextEncoder().encode(event.data).length > MAX_MESSAGE_BYTES) throw new Error("CDP frame exceeds bounds");
        const message = JSON.parse(event.data);
        if (!Number.isSafeInteger(message.id) || message.id <= 0 || typeof message.method !== "string" || message.method.length > 256 || binding.requests.has(message.id) || pendingCalls >= MAX_PENDING_CALLS || (message.sessionId !== undefined && message.sessionId !== SESSION_ID)) throw new Error("Invalid CDP request");
        binding.requests.add(message.id);
        pendingCalls++;
        // Capture the binding, never the mutable selected socket. No retry.
        Promise.resolve().then(() => {
          if (!current(binding)) throw new Error("Selection retired");
          return chrome.debugger.sendCommand({ tabId }, message.method, message.params || {});
        }).then((result) => {
          send(binding, { id: message.id, result: result || {} });
        }, () => {
          send(binding, { id: message.id, error: { code: -32000, message: "Chrome debugger command failed" } });
        }).finally(() => { pendingCalls--; binding.requests.delete(message.id); });
      } catch (_) { void unpin(binding); }
    };
    socket.onclose = () => { void unpin(binding); };
    socket.onerror = () => { void unpin(binding); };
  } catch (error) {
    await unpin(binding);
    throw error;
  }
}

async function select(tab) {
  if (selecting || retiring) return;
  selecting = true;
  try {
    const previous = selected;
    if (previous) await unpin(previous);
    if (previous?.tabId !== tab.id) await pin(tab.id);
  } catch (_) {
    // Chrome shows its own permission/debugger error; no implicit alternate tab.
  } finally { selecting = false; }
}

chrome.debugger.onEvent.addListener((source, method, params) => {
  const binding = selected;
  if (binding && source.tabId === binding.tabId) send(binding, { method, params: params || {} });
});
chrome.debugger.onDetach.addListener((source) => {
  const binding = selected;
  if (binding && source.tabId === binding.tabId) void unpin(binding);
});
chrome.action.onClicked.addListener(select);
setInterval(() => {
  if (selected) send(selected, { method: "whip.ping", params: {} });
}, 20000);

// Test/CI opt-in only. Production relay state never writes autoAttach. These
// retries select an initial tab, never repeat a CDP command or reconnect a pin.
async function maybeAutoAttach() {
  if (autoAttaching) return;
  autoAttaching = true;
  try {
    if (!(await readRelay()).autoAttach) return;
    for (let attempt = 0; attempt < 30 && !selected; attempt++) {
      const tabs = await chrome.tabs.query({});
      const tab = tabs.find((value) => value.url && !value.url.startsWith("chrome://")) || tabs[0];
      if (tab) await select(tab);
      if (!selected) await new Promise((resolve) => setTimeout(resolve, 500));
    }
  } catch (_) {} finally { autoAttaching = false; }
}
chrome.runtime.onInstalled.addListener(() => { void swlog("onInstalled"); void maybeAutoAttach(); });
chrome.runtime.onStartup.addListener(() => { void swlog("onStartup"); void maybeAutoAttach(); });
void maybeAutoAttach();
