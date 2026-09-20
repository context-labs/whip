// Renderer-only validation: the bridge below is a test double, not browser authority.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';
import { themeCatalog } from '../../../packages/ui/src/generated/theme-catalog.ts';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = resolve(process.env.WHIP_DESIGN_RESULTS ?? '/tmp/whip-browser-design-results');
assert.ok(!results.startsWith(resolve(web, '../..') + '/'), 'Keep screenshots/build artifacts outside the repository');
await mkdir(results, { recursive: true });
process.chdir(web);
const csp = (await readFile(resolve(web, '../../internal/webassets/csp.txt'), 'utf8')).trim();
const modules = [];
// Compile the real isolated production entry; avoid loading routes, SDK, or the app runtime.
const config = {
  configFile: false, root: web, logLevel: 'warn',
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react(), {
    name: 'design-renderer-boundary', generateBundle() {
      for (const id of this.getModuleIds()) {
        modules.push(id);
        assert.ok(!/packages\/sdk\/|browser-design-controller|app-context|__vite-browser-external|^node:/.test(id), `Runtime entered isolated renderer: ${id}`);
      }
    },
  }],
  build: { target: 'es2022', outDir: resolve(results, 'dist'), emptyOutDir: true, assetsInlineLimit: 0,
    rolldownOptions: { input: resolve(web, 'design.html') } },
  preview: { host: '127.0.0.1', port: 0, headers: { 'Content-Security-Policy': csp } },
};
await build(config);
await writeFile(resolve(results, 'modules.json'), JSON.stringify(modules, null, 2));
const server = await preview(config);
const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
const report = [], failures = [];
const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAUAAAABkCAIAAAB4uH5pAAABQUlEQVR4nO3TsQkAIBDAwN9/S0snsHUHGwkc3ABpMmsfIGq+FwDPDAxhBoYwA0OYgSHMwBBmYAgzMIQZGMIMDGEGhjADQ5iBIczAEGZgCDMwhBkYwgwMYQaGMANDmIEhzMAQZmAIMzCEGRjCDAxhBoYwA0OYgSHMwBBmYAgzMIQZGMIMDGEGhjADQ5iBIczAEGZgCDMwhBkYwgwMYQaGMANDmIEhzMAQZmAIMzCEGRjCDAxhBoYwA0OYgSHMwBBmYAgzMIQZGMIMDGEGhjADQ5iBIczAEGZgCDMwhBkYwgwMYQaGMANDmIEhzMAQZmAIMzCEGRjCDAxhBoYwA0OYgSHMwBBmYAgzMIQZGMIMDGEGhjADQ5iBIczAEGZgCDMwhBkYwgwMYQaGMANDmIEhzMAQZmAIMzCEGRjCDAxhBoawC9taUSkSFOYWAAAAAElFTkSuQmCC';
function model(width, theme) {
  return {
    state: { epoch: 'fixture-epoch', tabId: 'fixture-tab', generation: 1, designId: 'fixture-design',
      documentRevision: 2, selectionRevision: 3, status: 'active', viewport: { width, height: 800 }, elements: [] },
    draft: { prompt: '', recipients: [ { id: 'one', label: 'Design review', available: true },
      { id: 'two', label: 'Implementation', available: true }, { id: 'gone', label: 'Offline conversation', available: false } ],
      recipientId: 'one', screenshot: true, delivery: 'queue', busy: false, uncertain: false,
      evidence: { text: '{"source":"untrusted page", "selector":"button.save"}', image: pixel }, theme: { name: theme, mode: theme } },
  };
}
function element(number, bounds) {
  return { id: `element-${number}`, number, color: ['blue', 'purple', 'green', 'orange'][(number - 1) % 4],
    label: number === 4 ? 'button.save-with-an-intentionally-long-selector-that-must-truncate' : `button.target-${number}`, bounds };
}
async function update(page, state = {}, draft = {}) {
  await page.evaluate(({ state, draft }) => window.designFixture.update(state, draft), { state, draft });
}
async function intents(page, kind) {
  return page.evaluate(kind => window.designFixture.intents.filter(item => !kind || item.intent.kind === kind), kind);
}
// Freeze newly created browser animations in the mutation microtask, before the next
// paint. Tests seek the real engine animation clock, not a timeout approximation.
async function motionUpdate(page, state = {}, draft = {}) {
  return page.evaluate(async ({ state, draft }) => {
    window.designFixture.motion.sample(); // Commit the prior computed style for CSS transitions.
    window.designFixture.update(state, draft);
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
    return window.designFixture.motion.sample();
  }, { state, draft });
}
async function motionSeek(page, time) {
  return page.evaluate(time => {
    const motion = window.designFixture.motion;
    for (const animation of motion.node()?.getAnimations() ?? []) animation.currentTime = time;
    return motion.sample();
  }, time);
}
function closeBox(actual, expected, context = 'outline') {
  assert.ok(actual, `${context}: missing outline`);
  for (const key of ['x', 'y', 'width', 'height']) {
    assert.ok(Math.abs(actual[key] - expected[key]) <= 0.7,
      `${context} ${key}: expected ${expected[key]}, got ${actual[key]}`);
  }
}
function betweenBox(actual, start, end) {
  for (const key of ['x', 'y', 'width', 'height']) {
    const progress = (actual[key] - start[key]) / (end[key] - start[key]);
    assert.ok(progress > 0.55 && progress < 0.8, `${key} is not halfway ease-out interpolation: ${progress}`);
  }
}
async function geometry(page, width, elements) {
  await expect(async () => measureGeometry(page, width, elements)).toPass({ timeout: 1800 });
}
async function measureGeometry(page, width, elements) {
  const box = await page.getByRole('form', { name: 'Describe a design change' }).boundingBox();
  assert.ok(box, 'Composer missing');
  const expectedWidth = Math.min(360, width - 24);
  let x = width - expectedWidth - 12, y = 800 - box.height - 12;
  if (elements.length === 1) {
    x = elements[0].bounds.x;
    y = elements[0].bounds.y + elements[0].bounds.height + 12;
    if (y + box.height > 788) y = elements[0].bounds.y - box.height - 12;
  }
  x = Math.max(12, Math.min(x, width - expectedWidth - 12));
  y = Math.max(12, Math.min(y, 788 - box.height));
  for (const [key, expected] of Object.entries({ x, y, width: expectedWidth })) {
    assert.ok(Math.abs(box[key] - expected) <= 0.6, `${key}: expected ${expected}, got ${JSON.stringify(box)}`);
  }
  assert.ok(box.x >= 11.4 && box.y >= 11.4 && box.x + box.width <= width - 11.4 && box.y + box.height <= 788.6,
    `Composer escaped viewport: ${JSON.stringify(box)}`);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Horizontal overflow');
  return box;
}
try {
  for (const [engine, type] of Object.entries({ chromium, firefox })) {
    const browser = await type.launch();
    try {
      for (const width of [1280, 480, 320]) for (const theme of ['light', 'dark']) {
        const label = `${engine}-${width}-${theme}`;
        const page = await browser.newPage({ viewport: { width, height: 800 }, colorScheme: theme });
        const errors = [];
        page.on('pageerror', error => errors.push(error.message));
        page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
        const check = async (name, run) => {
          try { await run(); report.push({ layout: label, check: name, passed: true }); }
          catch (error) {
            const focus = await page.evaluate(() => document.activeElement?.outerHTML);
            failures.push({ layout: label, check: name, error: String(error), focus });
            await page.screenshot({ path: resolve(results, `${label}-failure-${failures.length}.png`), omitBackground: engine === 'chromium' });
            console.error(`${label}: ${name}: ${error.message}\nFocused: ${focus}`);
          }
        };
        await page.addInitScript(initial => {
          let current = initial;
          const listeners = new Set();
          const fixture = window.designFixture = {
            intents: [], cspErrors: [],
            update(state = {}, draft = {}) {
              current = { state: { ...current.state, ...state }, draft: { ...current.draft, ...draft } };
              for (const listener of listeners) listener(structuredClone(current));
            },
          };
          document.addEventListener('securitypolicyviolation', event => fixture.cspErrors.push({ directive: event.violatedDirective, blocked: event.blockedURI }));
          window.whipBrowserDesign = {
            snapshot: async () => structuredClone(current),
            onModel(listener) { listeners.add(listener); return () => listeners.delete(listener); },
            async intent(input) {
              fixture.intents.push(structuredClone(input));
              const action = input.intent;
              if (action.kind === 'prompt') fixture.update({}, { prompt: action.value });
              if (action.kind === 'recipient') fixture.update({}, { recipientId: action.id });
              if (action.kind === 'screenshot') fixture.update({}, { screenshot: action.value });
              if (action.kind === 'delivery') fixture.update({}, { delivery: action.value });
              if (action.kind === 'remove') fixture.update({ elements: current.state.elements.filter(item => item.id !== action.id) });
            },
          };
        }, model(width, theme));
        const response = await page.goto(`${origin}/design.html`);
        assert.equal(response.headers()['content-security-policy'], csp);
        await expect(page.getByRole('main', { name: 'Browser Design Mode' })).toBeVisible();
        await page.evaluate(() => document.fonts.ready);
        await check('transparent native guest background', async () => {
          const backgrounds = await page.evaluate(() => [document.documentElement, document.body, document.querySelector('main')].map(node => getComputedStyle(node).backgroundColor));
          assert.deepEqual(backgrounds, ['rgba(0, 0, 0, 0)', 'rgba(0, 0, 0, 0)', 'rgba(0, 0, 0, 0)']);
        });
        await check('keyboard picker navigation and additive selection', async () => {
          const picker = page.getByRole('button', { name: 'Pick an element', exact: true });
          await expect(picker).toBeFocused();
          for (const key of ['Tab', 'ArrowRight']) await picker.press(key);
          for (const key of ['Shift+Tab', 'ArrowLeft']) await picker.press(key);
          assert.equal((await intents(page, 'next')).length, 2);
          assert.equal((await intents(page, 'previous')).length, 2);
          await picker.press('Enter');
          await picker.press('Shift+Enter');
          await picker.press('Space');
          assert.deepEqual((await intents(page, 'pick-hover')).map(item => item.intent.additive), [false, true, false]);
          await picker.press('ArrowUp');
          assert.equal((await intents(page, 'ancestor')).length, 1);
          const before = (await intents(page)).length;
          await picker.dispatchEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true });
          assert.equal((await intents(page)).length, before);
        });
        await page.evaluate(() => {
          const root = document.querySelector('main');
          const node = () => root.querySelector('[data-design-hover]');
          const seen = new WeakSet();
          let previous;
          const freeze = () => {
            for (const animation of node()?.getAnimations() ?? []) if (!seen.has(animation)) {
              seen.add(animation); animation.pause(); animation.currentTime = 0;
            }
          };
          const observer = new MutationObserver(freeze);
          observer.observe(root, { attributes: true, childList: true, subtree: true });
          window.designFixture.motion = { node, observer, sample() {
            freeze();
            const current = node(), box = current?.getBoundingClientRect();
            const animations = current?.getAnimations() ?? [];
            const result = { bounds: box ? { x: box.x, y: box.y, width: box.width, height: box.height } : null,
              stable: !previous || previous === current,
              timing: animations.map(animation => ({ duration: animation.effect.getTiming().duration,
                easing: animation.effect.getTiming().easing, frames: animation.effect.getKeyframes() })),
              selectedAnimations: Array.from(root.children).filter(item => getComputedStyle(item).borderTopWidth === '2px')
                .flatMap(item => item.getAnimations()).length };
            previous = current;
            return result;
          } };
        });
        const motionA = element(1, { x: 20, y: 100, width: 60, height: 30 });
        const motionB = element(2, { x: 130, y: 240, width: 130, height: 80 });
        const motionC = element(3, { x: 55, y: 160, width: 95, height: 50 });
        await check('hover 100ms ease-out position/dimensions, stable node, continuous retarget and actual click intent', async () => {
          const initial = await motionUpdate(page, { hover: motionA, hoverGeometryRevision: 0 });
          closeBox(initial.bounds, motionA.bounds, 'first hover snaps');
          assert.equal(initial.timing.length, 0);
          const start = await motionUpdate(page, { hover: motionB });
          assert.ok(start.stable, 'Target change remounted hover node');
          assert.ok(start.timing.length > 0, 'Target change did not animate');
          for (const timing of start.timing) {
            assert.equal(timing.duration, 100);
            assert.ok(timing.easing === 'ease-out' || timing.frames.some(frame => frame.easing === 'ease-out'),
              `Expected ease-out: ${JSON.stringify(timing)}`);
          }
          closeBox(start.bounds, motionA.bounds, 'animation begins at prior bounds');
          const middle = await motionSeek(page, 50);
          betweenBox(middle.bounds, motionA.bounds, motionB.bounds);
          await page.screenshot({ path: resolve(results, `${label}-hover-midpoint.png`), animations: 'allow', omitBackground: engine === 'chromium' });
          // Pointer coordinates belong to the real target, never the moving decorative outline.
          await page.mouse.click(motionB.bounds.x + 10, motionB.bounds.y + 10);
          assert.deepEqual((await intents(page, 'pick')).at(-1).intent,
            { kind: 'pick', x: motionB.bounds.x + 10, y: motionB.bounds.y + 10, additive: false });
          const duplicate = await motionUpdate(page, { hover: motionB });
          closeBox(duplicate.bounds, middle.bounds, 'duplicate snapshot preserves in-flight position');
          assert.ok(duplicate.stable && duplicate.timing.length > 0);
          const retarget = await motionUpdate(page, { hover: motionC });
          assert.ok(retarget.stable, 'Retarget remounted hover node');
          closeBox(retarget.bounds, middle.bounds, 'retarget starts from displayed midpoint');
          assert.ok(retarget.timing.length > 0);
          const retargetMiddle = await motionSeek(page, 50);
          betweenBox(retargetMiddle.bounds, middle.bounds, motionC.bounds);
          closeBox((await motionSeek(page, 100)).bounds, motionC.bounds, '100ms endpoint');
          const picker = page.getByRole('button', { name: 'Pick an element' });
          await picker.focus();
          const nextCount = (await intents(page, 'next')).length;
          await picker.press('ArrowRight');
          assert.equal((await intents(page, 'next')).length, nextCount + 1);
          const keyboard = await motionUpdate(page, { hover: motionB });
          assert.ok(keyboard.timing.length > 0 && keyboard.stable, 'Keyboard target must animate the same outline');
          await picker.press('Enter');
          assert.deepEqual((await intents(page, 'pick-hover')).at(-1).intent, { kind: 'pick-hover', additive: false });
          await motionSeek(page, 100);
        });
        await check('same-target and scroll/resize/zoom epoch geometry snap; selection never animates', async () => {
          await motionUpdate(page, { hover: undefined });
          await motionUpdate(page, { hover: motionA, hoverGeometryRevision: 0 });
          const changed = { ...motionA, bounds: { x: 35, y: 120, width: 90, height: 42 } };
          const same = await motionUpdate(page, { hover: changed });
          closeBox(same.bounds, changed.bounds, 'same-target geometry');
          assert.equal(same.timing.length, 0);
          for (const [index, reason] of ['scroll', 'resize', 'zoom'].entries()) {
            const target = index % 2 ? motionA : motionB;
            const snap = await motionUpdate(page, { hover: target, hoverGeometryRevision: index + 1 });
            closeBox(snap.bounds, target.bounds, `${reason} epoch`);
            assert.equal(snap.timing.length, 0, `${reason} must not animate`);
          }
          await motionUpdate(page, { elements: [motionA], hover: motionA });
          const selected = await motionUpdate(page, { elements: [changed], hover: motionB });
          assert.equal(selected.selectedAnimations, 0, 'Selected outline acquired hover motion');
          const box = await page.getByRole('main').evaluate(root => {
            const item = Array.from(root.children).find(item => getComputedStyle(item).borderTopWidth === '2px');
            const rect = item.getBoundingClientRect();
            return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
          });
          closeBox(box, changed.bounds, 'selected bounds snap');
          await motionUpdate(page, { elements: [], hover: undefined });
        });
        await check('app and OS reduced motion snap including live preference changes', async () => {
          const display = { uiFont: 'system', codeFont: 'system', uiSize: 13, codeSize: 12,
            wrapCode: true, contrast: 'standard', motion: 'reduce' };
          try {
            for (const source of ['app', 'os']) {
              await page.emulateMedia({ reducedMotion: 'no-preference' });
              await motionUpdate(page, { hover: undefined }, { theme: { name: theme, mode: theme } });
              await motionUpdate(page, { hover: motionA });
              await motionUpdate(page, { hover: motionB });
              await motionSeek(page, 50);
              if (source === 'app') await motionUpdate(page, {}, { theme: { name: theme, mode: theme, display } });
              else {
                await page.emulateMedia({ reducedMotion: 'reduce' });
                await page.waitForFunction(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
                await motionUpdate(page, {}, {});
              }
              const live = await page.evaluate(() => window.designFixture.motion.sample());
              closeBox(live.bounds, motionB.bounds, `${source} live preference cancels motion`);
              assert.equal(live.timing.length, 0, `${source} live preference leaves no animation`);
              const reduced = await motionUpdate(page, { hover: motionC });
              closeBox(reduced.bounds, motionC.bounds, `${source} reduced motion`);
              assert.equal(reduced.timing.length, 0, `${source} must disable hover motion`);
            }
          } finally {
            await page.emulateMedia({ reducedMotion: 'no-preference' });
            await motionUpdate(page, { hover: undefined }, { theme: { name: theme, mode: theme } });
          }
        });
        await check('hover clear immediately removes an in-flight outline', async () => {
          await motionUpdate(page, { hover: motionA });
          await motionUpdate(page, { hover: motionB });
          await motionSeek(page, 50);
          const cleared = await motionUpdate(page, { hover: undefined });
          assert.equal(cleared.bounds, null);
          assert.equal(cleared.timing.length, 0);
          const reenter = await motionUpdate(page, { hover: motionC });
          closeBox(reenter.bounds, motionC.bounds, 'reenter snaps');
          assert.equal(reenter.timing.length, 0);
          await motionUpdate(page, { hover: motionB });
          await motionSeek(page, 50);
          const inactive = await motionUpdate(page, { status: 'stale' });
          assert.equal(inactive.bounds, null, 'Nonactive state retained decorative hover');
          await motionUpdate(page, { status: 'active', hover: motionA, hoverGeometryRevision: undefined });
          const legacy = await motionUpdate(page, { hover: motionB });
          closeBox(legacy.bounds, motionB.bounds, 'missing geometry revision snaps');
          assert.equal(legacy.timing.length, 0);
        });
        await page.evaluate(() => window.designFixture.motion.observer.disconnect());
        await update(page, { elements: [], hover: undefined, hoverGeometryRevision: 0 });
        await check('empty hover and picker intents', async () => {
          await expect(page.getByRole('form')).toHaveCount(0);
          await expect(page.getByText('Select an element to describe a change')).toBeVisible();
          const hover = element(1, { x: 30, y: 100, width: 140, height: 36 });
          await update(page, { hover });
          await expect(page.getByText('Click to select · Shift-click to add')).toBeVisible();
          await page.mouse.move(60, 115);
          await expect.poll(async () => (await intents(page, 'hover')).length).toBeGreaterThan(0);
          await page.keyboard.down('Shift');
          await page.mouse.click(60, 115);
          await page.keyboard.up('Shift');
          const pick = (await intents(page, 'pick')).at(-1);
          assert.deepEqual(pick.intent, { kind: 'pick', x: 60, y: 115, additive: true });
          assert.equal(pick.revision.designId, 'fixture-design');
          await page.screenshot({ path: resolve(results, `${label}-empty-hover.png`), omitBackground: engine === 'chromium' });
        });
        const single = [element(1, { x: width - 90, y: 730, width: 76, height: 42 })];
        await update(page, { elements: single, hover: undefined }, { prompt: 'Increase the spacing' });
        const form = page.getByRole('form', { name: 'Describe a design change' });
        const prompt = page.getByRole('textbox', { name: 'Describe the change' });
        await expect(form).toBeVisible();
        await check('single target flips above and clamps', async () => {
          await expect.poll(async () => (await form.boundingBox()).y).toBeLessThan(730);
          await geometry(page, width, single);
          await page.screenshot({ path: resolve(results, `${label}-single.png`), omitBackground: engine === 'chromium' });
          const below = [element(1, { x: 24, y: 80, width: 100, height: 32 })];
          await update(page, { elements: below });
          await expect.poll(async () => (await form.boundingBox()).y).toBe(124);
          await geometry(page, width, below);
        });
        const multiple = [1, 2, 3, 4].map(n => element(n, { x: 20 + (n - 1) * 50, y: 80 + n * 45, width: 80, height: 32 }));
        await update(page, { elements: multiple });
        await check('multiple lower-right numbered colored removable chips', async () => {
          await expect(page.getByRole('button', { name: /^Remove element/ })).toHaveCount(4);
          await geometry(page, width, multiple);
          const outlines = await page.getByRole('main').evaluate(node => Array.from(node.children)
            .filter(child => getComputedStyle(child).pointerEvents === 'none' && getComputedStyle(child).borderTopWidth === '2px')
            .map(child => { const box = child.getBoundingClientRect(); return { number: child.textContent, x: box.x, y: box.y, width: box.width, height: box.height }; }));
          assert.deepEqual(outlines, multiple.map(item => ({ number: String(item.number), ...item.bounds })));
          const colors = [];
          for (const item of multiple) {
            const chip = page.getByRole('button', { name: `Remove element ${item.number}: ${item.label}`, exact: true });
            await expect(chip.locator('b')).toHaveText(String(item.number));
            colors.push(await chip.evaluate(node => getComputedStyle(node).color));
          }
          assert.equal(new Set(colors).size, 4, `Selection colors must be distinct: ${colors}`);
          await page.screenshot({ path: resolve(results, `${label}-multiple.png`), omitBackground: engine === 'chromium' });
          await page.getByRole('button', { name: /^Remove element 4:/ }).click();
          await expect(page.getByRole('button', { name: /^Remove element/ })).toHaveCount(3);
          assert.equal((await intents(page, 'remove')).at(-1).intent.id, 'element-4');
        });
        await check('footer text alignment and clickable checkbox label cursor', async () => {
          const offsets = await form.evaluate(node => {
            const controls = [node.querySelector('label'),
              [...node.querySelectorAll('button')].find(button => button.textContent === 'Preview'),
              node.querySelector('[aria-label="Delivery when busy"]')];
            return controls.map(control => {
              const walker = document.createTreeWalker(control, NodeFilter.SHOW_TEXT);
              let text;
              while ((text = walker.nextNode()) && !text.textContent.trim()) {}
              const range = document.createRange();
              range.selectNodeContents(text);
              const glyphs = range.getBoundingClientRect(), box = control.getBoundingClientRect();
              return { text: text.textContent, offset: glyphs.y + glyphs.height / 2 - box.y - box.height / 2 };
            });
          });
          assert.ok(Math.max(...offsets.map(item => item.offset)) - Math.min(...offsets.map(item => item.offset)) < 0.6,
            `Footer text is not aligned within its controls: ${JSON.stringify(offsets)}`);
          const label = page.getByText('Include Screenshots', { exact: true });
          await label.hover();
          await expect(label).toHaveCSS('cursor', 'pointer');
          await label.click();
          await expect(page.getByRole('checkbox', { name: 'Include Screenshots' })).not.toBeChecked();
          await label.click();
          await expect(page.getByRole('checkbox', { name: 'Include Screenshots' })).toBeChecked();
        });
        await check('shared controls and bounded textarea growth', async () => {
          await expect(page.locator('select')).toHaveCount(0);
          await expect(page.getByRole('checkbox', { name: 'Include Screenshots' })).toBeChecked();
          await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeVisible();
          await expect(form.locator('svg.lucide-camera')).toHaveCount(0);
          await expect(prompt).toHaveCSS('resize', 'none');
          const send = page.getByRole('button', { name: 'Send design change', exact: true });
          const circle = await send.evaluate(node => {
            const box = node.getBoundingClientRect(), css = getComputedStyle(node);
            return { width: box.width, height: box.height, radius: css.borderTopLeftRadius };
          });
          assert.ok(Math.abs(circle.width - circle.height) < 0.6, JSON.stringify(circle));
          assert.ok(circle.radius === '50%' || parseFloat(circle.radius) >= circle.width / 2, JSON.stringify(circle));
          const card = await form.evaluate(node => {
            const css = getComputedStyle(node);
            return { border: css.borderTopWidth, shadow: css.boxShadow, radius: css.borderTopLeftRadius };
          });
          assert.equal(card.border, '1px');
          assert.notEqual(card.shadow, 'none');
          assert.equal(card.radius, '20px');
          await prompt.fill('Short change');
          const short = (await prompt.boundingBox()).height;
          await prompt.fill(Array.from({ length: 40 }, (_, i) => `Line ${i + 1}: preserve this selected component`).join('\n'));
          await expect.poll(async () => (await prompt.boundingBox()).height).toBeGreaterThan(short);
          const tall = await prompt.evaluate(node => ({ height: node.clientHeight, content: node.scrollHeight, max: parseFloat(getComputedStyle(node).maxHeight) }));
          assert.ok(tall.height <= tall.max + 1 && tall.content > tall.height, JSON.stringify(tall));
          await geometry(page, width, multiple.slice(0, 3));
          await page.screenshot({ path: resolve(results, `${label}-long-prompt.png`), omitBackground: engine === 'chromium' });
          await prompt.fill('Short change');
          await expect.poll(async () => (await prompt.boundingBox()).height).toBe(short);
        });
        await check('dropdown portals clamp long names and preserve keyboard focus without page intents', async () => {
          const recipients = model(width, theme).draft.recipients;
          const longName = 'Design system review — navigation accessibility and responsive component implementation';
          await update(page, {}, { recipients: recipients.map(item => item.id === 'one' ? { ...item, label: longName } : item) });
          await page.evaluate(() => { window.designFixture.intents = []; });
          const to = page.getByRole('combobox', { name: 'Conversation', exact: true });
          try {
            const chevron = await to.evaluate(node => {
              const trigger = node.getBoundingClientRect(), icon = node.querySelector('svg').getBoundingClientRect();
              return { trigger: { x: trigger.x, right: trigger.right }, icon: { x: icon.x, right: icon.right, width: icon.width } };
            });
            assert.ok(chevron.icon.width > 0 && chevron.icon.x >= chevron.trigger.x && chevron.icon.right <= chevron.trigger.right, `Long conversation clipped dropdown affordance: ${JSON.stringify(chevron)}`);
            await to.focus();
            await to.press('Enter');
            const menu = page.getByRole('listbox');
            await expect(menu).toBeVisible();
            await expect(page.getByRole('option', { name: longName, exact: true })).toBeVisible();
            const box = await menu.boundingBox();
            assert.ok(box.x >= -0.6 && box.y >= -0.6 && box.x + box.width <= width + 0.6 && box.y + box.height <= 800.6, `Dropdown escaped viewport: ${JSON.stringify(box)}`);
            await menu.dispatchEvent('wheel', { deltaY: 60, bubbles: true });
            await page.screenshot({ path: resolve(results, `${label}-conversation-dropdown.png`), omitBackground: engine === 'chromium' });
            await page.keyboard.press('Escape');
            await expect(menu).toHaveCount(0);
            await expect(to).toBeFocused();
            await to.press('Enter');
            await expect(menu).toBeVisible();
            await page.keyboard.press('Home');
            await expect(page.getByRole('option', { name: longName, exact: true })).toHaveAttribute('data-highlighted', '');
            await expect(page.getByRole('option', { name: longName, exact: true })).toBeFocused();
            await page.keyboard.press('ArrowDown');
            await expect(page.getByRole('option', { name: 'Implementation', exact: true })).toHaveAttribute('data-highlighted', '');
            await page.keyboard.press('Enter');
            await expect(menu).toHaveCount(0);
            await expect(to).toContainText('Implementation');
            await expect(to).toBeFocused();
            const delivery = page.getByRole('combobox', { name: 'Delivery when busy' });
            await delivery.press('Enter');
            await expect(menu).toBeVisible();
            const deliveryBox = await menu.boundingBox();
            assert.ok(deliveryBox.x >= -0.6 && deliveryBox.y >= -0.6 && deliveryBox.x + deliveryBox.width <= width + 0.6 && deliveryBox.y + deliveryBox.height <= 800.6, JSON.stringify(deliveryBox));
            await page.keyboard.press('Escape');
            await expect(delivery).toBeFocused();
            await to.click();
            await expect(menu).toBeVisible();
            // An outside click dismisses the portal, never selects the page beneath it.
            await page.mouse.click(4, 4);
            await expect(menu).toHaveCount(0);
            for (const kind of ['pick', 'scroll', 'ancestor', 'stop', 'next', 'previous', 'pick-hover']) assert.equal((await intents(page, kind)).length, 0, `Dropdown emitted page ${kind}`);
            await geometry(page, width, multiple.slice(0, 3));
          } finally {
            await page.keyboard.press('Escape');
            await update(page, {}, { recipients, recipientId: 'one' });
          }
        });
        await check('local prompt ignores delayed ABA and empty echoes until explicit reset', async () => {
          for (const text of ['A', 'AB', 'A']) await prompt.fill(text);
          for (const echo of ['AB', '', 'A', 'AB']) {
            await update(page, {}, { prompt: echo });
            await expect(prompt).toHaveValue('A');
          }
          await update(page, {}, { prompt: '', promptReset: 1 });
          await expect(prompt).toHaveValue('');
        });
        await check('composer isolation, recipients, delivery and composition-safe send', async () => {
          await page.evaluate(() => { window.designFixture.intents = []; });
          await prompt.fill('Preserve this draft');
          await prompt.click();
          await prompt.press('Backspace');
          await prompt.dispatchEvent('wheel', { deltaY: 90, bubbles: true });
          await page.getByRole('combobox', { name: 'Conversation', exact: true }).click();
          await expect(page.getByRole('option', { name: /Offline conversation/ })).toBeDisabled();
          await page.getByRole('option', { name: 'Implementation', exact: true }).click();
          await page.getByRole('combobox', { name: 'Delivery when busy' }).click();
          await page.getByRole('option', { name: 'Steer', exact: true }).click();
          assert.equal((await intents(page, 'recipient')).at(-1).intent.id, 'two');
          assert.equal((await intents(page, 'delivery')).at(-1).intent.value, 'steer');
          await prompt.dispatchEvent('compositionstart');
          await prompt.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', ctrlKey: true, isComposing: true, bubbles: true });
          await prompt.dispatchEvent('keydown', { key: 'Escape', code: 'Escape', isComposing: true, bubbles: true });
          await prompt.dispatchEvent('compositionend');
          assert.equal((await intents(page, 'send')).length, 0, 'IME committed a send');
          assert.equal((await intents(page, 'stop')).length, 0, 'IME closed design mode');
          for (const kind of ['pick', 'scroll', 'ancestor']) assert.equal((await intents(page, kind)).length, 0, `Composer emitted ${kind}`);
          await prompt.press('Control+Enter');
          await expect.poll(async () => (await intents(page, 'send')).length).toBe(1);
          const sent = await intents(page);
          const index = sent.findIndex(item => item.intent.kind === 'send');
          assert.deepEqual(sent[index - 1].intent, { kind: 'prompt', value: await prompt.inputValue() });
          await expect(prompt).toBeDisabled();
          await expect(page.getByRole('button', { name: 'Sending design change', exact: true })).toBeDisabled();
          await prompt.dispatchEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true });
          assert.equal((await intents(page, 'send')).length, 1, 'Duplicate send before native acknowledgement');
          // Simulate the controller acknowledging and finishing this fixture-only send.
          await update(page, {}, { busy: true });
          await expect(page.getByRole('button', { name: 'Sending design change' })).toBeDisabled();
          await update(page, {}, { busy: false });
          await expect(prompt).toBeEnabled();
        });
        await check('screenshot evidence preview and removal', async () => {
          await page.getByRole('button', { name: 'Preview', exact: true }).click();
          await expect(page.getByRole('region', { name: 'Design evidence' })).toBeVisible();
          const image = page.getByRole('img', { name: 'Viewport screenshot to attach' });
          await expect(image).toBeVisible();
          await expect.poll(() => image.evaluate(node => node.complete && node.naturalWidth > 0)).toBe(true);
          await page.screenshot({ path: resolve(results, `${label}-evidence.png`), omitBackground: engine === 'chromium' });
          await page.getByRole('checkbox', { name: 'Include Screenshots' }).uncheck();
          await expect(image).toHaveCount(0);
          assert.equal((await intents(page, 'screenshot')).at(-1).intent.value, false);
          await expect(page.getByRole('region', { name: 'Design evidence' })).toContainText('untrusted page');
          await prompt.press('Escape');
          await expect(page.getByRole('region', { name: 'Design evidence' })).toHaveCount(0);
          assert.equal((await intents(page, 'evidence-close')).length, 1);
        });
        await check('busy disables changes and recoverable error preserves Preview and draft', async () => {
          const value = await prompt.inputValue();
          await update(page, {}, { screenshot: true });
          await page.getByRole('button', { name: 'Preview', exact: true }).click();
          await update(page, {}, { busy: true });
          await expect(prompt).toBeDisabled();
          await expect(page.getByRole('combobox', { name: 'Conversation', exact: true })).toBeDisabled();
          await expect(page.getByRole('combobox', { name: 'Delivery when busy' })).toBeDisabled();
          await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeDisabled();
          await expect(page.getByRole('checkbox', { name: 'Include Screenshots' })).toBeDisabled();
          await expect(page.getByText('Include Screenshots', { exact: true })).toHaveCSS('cursor', 'not-allowed');
          await expect(page.getByRole('region', { name: 'Design evidence' })).toBeVisible();
          await page.screenshot({ path: resolve(results, `${label}-busy-preview.png`), omitBackground: engine === 'chromium' });
          await update(page, {}, { busy: false, error: 'Could not send the design change. Try again.' });
          await page.getByRole('button', { name: 'Preview', exact: true }).click();
          await expect(page.getByRole('region', { name: 'Design evidence' })).toHaveCount(0);
          await expect(page.getByRole('alert')).toContainText('Could not send the design change. Try again.');
          await expect(prompt).toHaveValue(value);
          await expect(prompt).toBeEnabled();
          await expect(page.getByRole('button', { name: 'Send design change', exact: true })).toBeEnabled();
          await geometry(page, width, multiple.slice(0, 3));
          await page.screenshot({ path: resolve(results, `${label}-error.png`), omitBackground: engine === 'chromium' });
          await update(page, {}, { error: undefined });
        });
        await check('busy uncertain stale preserve draft and disable send', async () => {
          const value = await prompt.inputValue();
          await update(page, {}, { busy: true });
          await expect(page.getByRole('button', { name: 'Sending design change' })).toBeDisabled();
          await expect(prompt).toBeDisabled();
          await update(page, {}, { busy: false, uncertain: true });
          await expect(page.getByRole('status')).toContainText('Delivery is unconfirmed');
          await expect(page.getByRole('button', { name: 'Send design change', exact: true })).toBeDisabled();
          await page.screenshot({ path: resolve(results, `${label}-uncertain.png`), omitBackground: engine === 'chromium' });
          await update(page, { status: 'stale', elements: [], documentRevision: 3 }, { uncertain: false });
          await expect(page.getByRole('status')).toContainText('The page changed');
          await expect(prompt).toHaveValue(value);
          await expect(page.getByRole('button', { name: 'Send design change', exact: true })).toBeDisabled();
          await geometry(page, width, []);
          await page.screenshot({ path: resolve(results, `${label}-stale.png`), omitBackground: engine === 'chromium' });
        });
        await check('custom palette and display preferences mirror without hover rewrites', async () => {
          const palette = structuredClone(themeCatalog.find(item => item.dark));
          palette.id = 'custom-design-fixture';
          palette.name = 'Custom design fixture';
          palette.colors.background = '#102030';
          palette.colors.element = '#203040';
          palette.colors.foreground = '#f4f8fc';
          const display = { uiFont: 'system', codeFont: 'system', uiSize: 20, codeSize: 24,
            wrapCode: true, contrast: 'more', motion: 'reduce' };
          try {
            await update(page, {}, { theme: { name: palette.id, mode: 'dark', palette, display } });
            await expect(page.locator('html')).toHaveAttribute('data-theme', palette.id);
            await expect(page.locator('html')).toHaveAttribute('data-contrast', 'more');
            await expect(page.locator('html')).toHaveAttribute('data-motion', 'reduce');
            await expect(page.getByRole('main')).toHaveCSS('font-size', '20px');
            await expect(form).toHaveCSS('background-color', 'rgb(32, 48, 64)');
            const appearance = await page.evaluate(() => {
              const root = getComputedStyle(document.documentElement), main = getComputedStyle(document.querySelector('main'));
              return { font: main.fontFamily, size: main.fontSize, codeSize: root.getPropertyValue('--whip-code-size'),
                wrap: root.getPropertyValue('--whip-code-white-space'), motion: root.getPropertyValue('--whip-motion-normal'),
                background: root.backgroundColor, composer: getComputedStyle(document.querySelector('form')).backgroundColor, rawSize: root.getPropertyValue('--whip-size-13') };
            });
            assert.ok(!appearance.font.includes('Inter'), appearance.font);
            assert.equal(appearance.size, '20px', JSON.stringify(appearance));
            assert.equal(appearance.codeSize, '24px');
            assert.equal(appearance.wrap, 'pre-wrap');
            assert.equal(appearance.motion, '0ms');
            assert.equal(appearance.background, 'rgba(0, 0, 0, 0)');
            assert.equal(appearance.composer, 'rgb(32, 48, 64)');
            await update(page, { status: 'active', elements: multiple, documentRevision: 4 });
            await expect(page.getByRole('button', { name: /^Remove element/ })).toHaveCount(4);
            await geometry(page, width, multiple);
            const escaped = await form.evaluate(node => {
              const form = node.getBoundingClientRect();
              return Array.from(node.querySelectorAll('button, textarea, [role="checkbox"]'))
                .filter(control => { const box = control.getBoundingClientRect(); return box.width && (box.x < form.x - 0.6 || box.right > form.right + 0.6); })
                .map(control => control.getAttribute('aria-label') || control.textContent);
            });
            assert.deepEqual(escaped, [], 'Custom-font controls overflow the card');
            await page.screenshot({ path: resolve(results, `${label}-custom-font.png`), omitBackground: engine === 'chromium' });
            const to = page.getByRole('combobox', { name: 'Conversation', exact: true });
            await to.click();
            const menu = page.getByRole('listbox');
            await expect(menu).toBeVisible();
            const menuBox = await menu.boundingBox();
            assert.ok(menuBox.x >= -0.6 && menuBox.y >= -0.6 && menuBox.x + menuBox.width <= width + 0.6 && menuBox.y + menuBox.height <= 800.6, JSON.stringify(menuBox));
            await page.screenshot({ path: resolve(results, `${label}-custom-font-dropdown.png`), omitBackground: engine === 'chromium' });
            await page.keyboard.press('Escape');
            await expect(to).toBeFocused();
            await page.evaluate(() => {
              window.designFixture.appearanceMutations = [];
              window.designFixture.appearanceObserver = new MutationObserver(records => window.designFixture.appearanceMutations.push(...records.map(item => item.attributeName)));
              window.designFixture.appearanceObserver.observe(document.documentElement, { attributes: true });
            });
            await update(page, { hover: element(1, { x: 30, y: 100, width: 80, height: 32 }) });
            await expect(page.getByText('Click to select · Shift-click to add')).toBeVisible();
            assert.deepEqual(await page.evaluate(() => window.designFixture.appearanceMutations), []);
          } finally {
            await page.evaluate(() => window.designFixture.appearanceObserver?.disconnect());
            await update(page, { hover: undefined }, { theme: { name: theme, mode: theme } });
            await expect(page.locator('html')).toHaveAttribute('data-motion', 'system');
            await expect(page.locator('html')).toHaveAttribute('data-contrast', 'standard');
          }
        });
        await check('no JavaScript errors or production CSP violations', async () => {
          assert.deepEqual(errors, []);
          assert.deepEqual(await page.evaluate(() => window.designFixture.cspErrors), []);
        });
        await page.close();
      }
    } finally { await browser.close(); }
  }
} finally {
  await writeFile(resolve(results, 'report.json'), JSON.stringify({ report, failures, limitation: 'Renderer fixture only. No native guest compositor, real capture/upload, daemon admission, actual IME, Safari, or VoiceOver proof.' }, null, 2) + '\n');
  await new Promise(resolve => server.httpServer.close(resolve));
}
console.log(`${report.length} checks passed; ${failures.length} failed. Artifacts: ${results}`);
assert.equal(failures.length, 0, JSON.stringify(failures, null, 2));
