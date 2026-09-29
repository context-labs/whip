import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually } from './native-fixture.mjs';
import { startSkillsFixture } from './native-skills-fixture.mjs';
import { skillsTransport } from './native-skills-transport.mjs';

const directory = process.env.WHIP_SLASH_SKILLS_RESULTS ?? '/tmp/whip-native-slash-skills';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let seed, fixture, browser, page, proxy;
  const errors = [], csp = [], checks = [];
  const report = { name, checks, errors, csp, metrics: [], coverage: 'Production native assets and actual runtime; exact named skill roots and builtin definitions and explicit grants; real metadata replies delayed, no fabricated catalog or provider result' };
  reports.push(report);
  const record = (values, value) => { if (values.length < 64) values.push(String(value).slice(0, 4096)); else report.overflow = true; };
  try {
    seed = await startSkillsFixture();
    ({ fixture } = seed);
    const { client, definition, cwd, globalSkills, projectCatalog } = seed;
    const effectsBefore = await fixture.effects();
    assert.deepEqual((await client.listProviders(deadline())).routes, []);
    const globalPreview = await client.completeHostSkills({ scope: 'global', cwd: '', definition: definition.ref, prefix: '', limit: 1024 }, deadline());
    assert.deepEqual(globalPreview.candidates.map(candidate => candidate.text), globalSkills.map(skill => '$' + skill));
    assert.equal(globalPreview.truncated, false);
    const preview = await client.completeHostSkills({ scope: 'project', cwd, definition: definition.ref, prefix: '', limit: 1024 }, deadline());
    assert.deepEqual(preview.candidates.map(candidate => candidate.text), projectCatalog.map(skill => '$' + skill));
    assert.equal(preview.truncated, false);
    assert.deepEqual(await fixture.effects(), effectsBefore);
    report.catalogSize = preview.candidates.length; report.globalCatalogSize = globalPreview.candidates.length;
    checks.push('native global preview lists exact100 names from two explicitly published native roots; project preview adds exact100 names; no process-cwd leakage, provider requirement or effects');
    browser = await { chromium, firefox }[name].launch(); report.browser = browser.version();
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light' }); context.setDefaultTimeout(15000);
    page = await context.newPage(); page.on('pageerror', error => record(errors, error.stack ?? error));
    await page.exposeBinding('__skillCSP', (_source, event) => record(csp, event));
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => window.__skillCSP(event.violatedDirective + ': ' + event.blockedURI)));
    proxy = await skillsTransport(page); const { frames, responses } = proxy; let catalogStart = 0;
    const origin = fixture.info.web;
    const first = page.getByLabel('Your first message', { exact: true });
    const input = page.getByLabel('Message WHIP', { exact: true });
    const list = page.getByRole('listbox', { name: 'Skills', exact: true });
    const options = list.getByRole('option');
    const commands = method => frames.filter(frame => frame.method === method);
    const sends = () => frames.filter(frame => ['trees.create', 'sessions.submit'].includes(frame.method));
    const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
    const open = async (field, text = '/accept-al') => {
      await field.fill(text);
      // Reload restores the same draft. Native caret motion observes it even
      // when fill has no changed value and therefore emits no input event.
      await field.press('ArrowLeft');
      await field.press('ArrowRight');
      await expect(options).toHaveCount(text === '/accept-al' ? 2 : 32);
    };
    const catalogAcceptance = async (field, method, composer, global = false) => {
      const stem = global ? 'global' : 'accept';
      const catalog = global ? globalSkills : projectCatalog;
      const requests = () => frames.slice(catalogStart).filter(frame => frame.method === method && (frame.params.scope === 'global') === global && (method !== 'host.skills.complete' || frame.params.definition?.id === definition.ref.id));
      const submitted = sends().length;
      await field.evaluate(element => { window.__slashAcceptanceFocus = { element, id: element.id }; });
      await field.focus();
      await expect(field).toBeFocused();
      await eventually(() => requests().length > 0);
      assert.equal(await field.inputValue(), '', `${composer}: focus preloads before slash typing`);
      assert.equal(requests().filter(request => !request.params.after).length, 1, `${composer}: one deduplicated preload`);
      assert.equal(requests()[0].params.prefix, '');
      assert.equal(requests()[0].params.limit, method === 'skills.list' ? 100 : 1024);
      await field.fill(`/${stem}-z`);
      await expect(field).toHaveValue(`/${stem}-z`);
      await expect(page.getByText('Loading skills…', { exact: true })).toBeVisible();
      await field.press('Enter');
      assert.equal(sends().length, submitted, `${composer}: pending Enter never submits`);
      await expect(field).toHaveValue(`/${stem}-z`);
      await screenshot(`${composer}-cold`);
      await expect(options).toHaveCount(1);
      await expect(options.first()).toContainText(`/${stem}-zebra`);
      const delivered = requests().map(request => responses.find(response => response.id === request.id && response.connection === request.connection));
      const response = delivered[0];
      assert.ok(response && response.deliveredAt - response.sentAt >= 1500);
      if (method === 'skills.list') {
        assert.equal(requests().length, 2, 'One preload uses two bounded canonical pages');
        assert.deepEqual(delivered.flatMap(response => response.frame.result.items.map(item => '$' + item.name)), catalog.map(skill => '$' + skill));
        assert.equal(requests()[1].params.after, delivered[0].frame.result.next_after);
        assert.equal(delivered[1].frame.result.next_after, null);
      } else {
        assert.deepEqual(response.frame.result.candidates.map(candidate => candidate.text), catalog.map(skill => '$' + skill));
        assert.equal(response.frame.result.truncated, false);
      }
      await screenshot(`${composer}-beyond-64`);
      const before = requests().length;
      // Dispatch native input events into the real controlled textarea. Observe
      // the DOM on the next animation frame, excluding Playwright round trips.
      // Keep the menu open >10s to cover freshness expiry during continued typing.
      const warm = await field.evaluate(async (element, stem) => {
        const cases = [
          ['/', 32, `/${stem}-alpha`], [`/${stem}-a`, 2, `/${stem}-alpha`],
          [`/${stem}-alp`, 2, `/${stem}-alpha`], [`/${stem}-alpha`, 1, `/${stem}-alpha`],
          [`/${stem}-al`, 2, `/${stem}-alpha`], [`/${stem}-z`, 1, `/${stem}-zebra`],
          [`/${stem}-NONMATCH`, 0, null], [`/${stem}-long-95`, 1, `/${stem}-long-95`],
        ];
        let loadingFlashes = 0;
        const loading = () => { if (document.body.textContent.includes('Loading skills')) loadingFlashes++; };
        const observer = new MutationObserver(loading);
        observer.observe(document.body, { subtree: true, childList: true, characterData: true });
        const samples = [];
        const start = performance.now();
        try {
          for (let i = 0; performance.now() - start < 11_000; i++) {
            const [text, count, first] = cases[i % cases.length];
            const changedAt = performance.now();
            Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(element, text);
            element.setSelectionRange(text.length, text.length);
            element.dispatchEvent(new Event('input', { bubbles: true }));
            await new Promise(resolve => requestAnimationFrame(resolve));
            const rows = [...document.querySelectorAll('[role="listbox"][aria-label="Skills"] [role="option"]')];
            loading();
            samples.push({ text, milliseconds: performance.now() - changedAt, count: rows.length, expectedCount: count, first: rows[0]?.textContent ?? null, expectedFirst: first });
            await new Promise(resolve => setTimeout(resolve, 150));
          }
        } finally { observer.disconnect(); }
        return { samples, loadingFlashes, durationMs: performance.now() - start };
      }, stem);
      report.metrics.push({ composer, coldMs: response.deliveredAt - response.sentAt, warmRequests: requests().length - before, ...warm });
      assert.equal(warm.loadingFlashes, 0, `${composer}: no warm loading flashes`);
      assert.equal(requests().length, before, `${composer}: no warm RPCs, even beyond freshness expiry`);
      for (const sample of warm.samples) {
        assert.equal(sample.count, sample.expectedCount, `${composer}: next-frame rows for ${sample.text}`);
        if (sample.expectedFirst) assert.ok(sample.first?.includes(sample.expectedFirst), `${composer}: next-frame first candidate for ${sample.text}`);
      }
      assert.equal(sends().length, submitted, `${composer}: filtering never submits`);
      checks.push(`${composer}: focus preload; real 1500ms delayed cold response; pending editing/Enter; >64 discovery; next-frame local filtering before 32-row cap; no RPC/loading flashes through 11s typing`);
    };
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}`);
    await expect(page.getByRole('heading', { name: 'Connect a provider to get started', exact: true })).toBeVisible();
    await expect(page.locator('[data-startup-phase="visible"]')).toBeVisible();
    assert.equal(await first.count(), 0, 'Primary onboarding remains visible until explicit drafting');
    await page.getByRole('button', { name: 'Draft before connecting', exact: true }).click();
    await expect(first).toBeEnabled();
    assert.match(new URL(page.url()).pathname, /^\/new\//);
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveText('Choose folder');
    await catalogAcceptance(first, 'host.skills.complete', 'global-new-chat', true);
    const globalRequest = frames.find(frame => frame.method === 'host.skills.complete' && frame.params.definition?.id === definition.ref.id);
    assert.deepEqual(globalRequest.params, { scope: 'global', cwd: '', definition: definition.ref, prefix: '', limit: 1024 });
    await first.fill('/accept-'); await expect(options).toHaveCount(0);
    await expect(page.getByText('No matching skills.', { exact: true })).toBeVisible();
    await first.fill('/global-beta'); await expect(options).toHaveCount(1); await options.first().click();
    await expect(first).toHaveValue('$global-beta '); await expect(first).toBeFocused();
    assert.equal(await first.evaluate(element => element.selectionStart), '$global-beta '.length);
    await expect(page.getByRole('button', { name: 'Send first message', exact: true })).toBeDisabled();
    assert.deepEqual(sends(), []);
    assert.equal(frames.some(frame => ['sessions.get', 'sessions.history_page', 'sessions.turns'].includes(frame.method)), false);
    assert.deepEqual(await fixture.effects(), effectsBefore);
    await screenshot('global-no-folder-no-provider');
    checks.push('explicit folderless/no-provider draft lists only named globals; selection preserves caret/focus, with zero creates, submissions, session hydration or model effects');
    const projectDraft = decodeURIComponent(new URL(page.url()).pathname.split('/').at(-1));
    await page.getByRole('button', { name: 'Project folder', exact: true }).click();
    const folderDialog = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
    await folderDialog.getByRole('button', { name: 'Edit path', exact: true }).click();
    await folderDialog.getByRole('textbox', { name: 'Remote path', exact: true }).fill(cwd);
    await folderDialog.getByRole('button', { name: 'Go', exact: true }).click();
    await folderDialog.getByRole('button', { name: 'Choose folder', exact: true }).click();
    await expect(folderDialog).toHaveCount(0); await expect(first).toHaveValue('$global-beta ');
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', cwd);
    await first.fill(''); await catalogAcceptance(first, 'host.skills.complete', 'new-chat');
    await first.fill('/global-beta'); await expect(options).toHaveCount(1); await expect(options.first()).toContainText('/global-beta');
    checks.push('actual folder selection preserves text and changes exact native scope to project-plus-global metadata');
    await open(first, '/'); await expect(list).toBeVisible(); assert.equal(await first.getAttribute('aria-haspopup'), 'listbox');
    await open(first); await first.press('Enter'); await expect(first).toHaveValue('$accept-alpha ');
    await expect(list).toHaveCount(0); await expect(first).toBeFocused();
    assert.deepEqual(sends(), []); assert.deepEqual(await fixture.effects(), effectsBefore);
    const hostRequest = frames.filter(frame => frame.method === 'host.skills.complete').at(-1);
    assert.deepEqual(hostRequest.params, { scope: 'project', cwd, definition: definition.ref, prefix: '', limit: 1024 });
    await expect(page.getByRole('button', { name: 'Send first message', exact: true })).toBeDisabled();
    assert.equal((await client.listProviders(deadline())).defaults, null);
    await screenshot('new-session-no-provider');
    checks.push('project completion inserts canonical reference but Send remains disabled without a configured model');
    const globalCallsBefore = frames.filter(frame => frame.method === 'host.skills.complete' && frame.params.scope === 'global' && frame.params.definition?.id === definition.ref.id).length;
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New session', exact: true }).click();
    await page.getByRole('button', { name: 'Draft before connecting', exact: true }).click();
    await expect(first).toHaveValue(''); await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveText('Choose folder');
    await first.focus();
    await eventually(() => frames.filter(frame => frame.method === 'host.skills.complete' && frame.params.scope === 'global' && frame.params.definition?.id === definition.ref.id).length === globalCallsBefore + 1);
    const freshGlobal = frames.filter(frame => frame.method === 'host.skills.complete').at(-1);
    assert.deepEqual(freshGlobal.params, globalRequest.params);
    await first.fill('/accept-');
    await eventually(() => responses.some(response => response.id === freshGlobal.id && response.connection === freshGlobal.connection));
    await expect(page.getByText('No matching skills.', { exact: true })).toBeVisible(); await expect(options).toHaveCount(0);
    await first.fill('/global-alpha'); await expect(options).toHaveCount(1);
    await screenshot('fresh-folderless-draft');
    await page.locator(`[role="tab"][id="whip-workspace-tab-${projectDraft}"]`).click();
    // Returning to the existing draft preserves authored data; explicit drafting
    // remains a per-mounted-pane presentation choice, not a host configuration.
    if (await page.getByRole('button', { name: 'Draft before connecting', exact: true }).count()) await page.getByRole('button', { name: 'Draft before connecting', exact: true }).click();
    await expect(first).toHaveValue('$accept-alpha ');
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', cwd);
    assert.deepEqual(sends(), []); assert.deepEqual(await fixture.effects(), effectsBefore);
    checks.push('a fresh folderless draft cannot reuse project candidates; the original draft and exact definition survive navigation');
    await seed.configureProvider(); await page.reload(); await expect(first).toHaveValue('$accept-alpha ');
    await first.fill('$accept-alpha Synthetic first message');
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await expect(input).toBeEnabled(); await eventually(() => commands('sessions.submit').length === 1);
    assert.equal(commands('trees.create').length, 1);
    const creation = commands('trees.create')[0]; assert.deepEqual(creation.params.definition, definition.ref);
    assert.equal(creation.params.working_directory, cwd);
    const submission = commands('sessions.submit')[0], root = submission.params.session_id;
    assert.match(new URL(page.url()).pathname, /\/s\//);
    assert.deepEqual(submission.params.parts, [{ type: 'text', text: '$accept-alpha Synthetic first message' }]);
    // Observing the outgoing frame does not mean admission has committed. Wait
    // for this connection's exact ACK before inspecting its durable receipt.
    const acknowledged = await eventually(() => responses.find(response => response.connection === submission.connection && response.id === submission.id && response.method === 'sessions.submit'), { description: 'the exact first submission acknowledgement' });
    assert.equal(acknowledged.frame.error, undefined, 'First submission must be acknowledged successfully');
    assert.deepEqual(acknowledged.frame.result.receipt.identity, submission.params.identity);
    assert.equal(acknowledged.frame.result.input.session_id, root);
    // A queued ACK legitimately has no turn yet. The existing SDK receipt wait
    // observes this exact client/request until claim and settlement; it never sends.
    const submitter = await fixture.connect(submission.params.identity.client_id);
    const firstDone = await submitter.wait(submission.params.identity.request_id, deadline());
    assert.equal(firstDone.input.id, acknowledged.frame.result.input.id);
    assert.equal(firstDone.turn.state, 'succeeded'); assert.equal(firstDone.turn.session_id, root);
    assert.equal(firstDone.input.turn_id, firstDone.turn.id);
    if (acknowledged.frame.result.turn) assert.equal(firstDone.turn.id, acknowledged.frame.result.turn.id);
    const ungranted = await client.call('turns.instructions', { turn_id: firstDone.turn.id }, deadline());
    assert(!ungranted.manifest.sources.some(source => ['skill_metadata', 'invoked_skill'].includes(source.kind)), 'Ungrantable sources omitted before any file read');
    assert.equal((await client.call('skills.list', { session_id: root, prefix: '', limit: 100 }, deadline())).items.length, 0);
    assert.equal(firstDone.input.session_id, root);
    checks.push('only explicit Send creates and submits once; an ungranted skill remains literal, with no captured skill catalog or body');
    await seed.grant(root);
    const command = client.session(root).submission([{ type: 'text', text: '$accept-alpha Explicitly granted fresh invocation' }], randomUUID());
    await command.send(deadline()); const done = await command.wait(deadline()); assert.equal(done.turn.state, 'succeeded');
    const invoked = await client.call('turns.instructions', { turn_id: done.turn.id }, deadline());
    assert(invoked.manifest.sources.some(source => source.kind === 'invoked_skill' && source.path.includes('accept-alpha')));
    assert(invoked.manifest.sources.some(source => source.kind === 'skill_metadata' && source.root_id === 'whip'));
    checks.push('separate explicit exact-root grants authorize a fresh invocation; immutable captured manifest proves selected skill body, without replaying accepted work');
    const settledEffects = await fixture.effects(), sendCount = sends().length;
    catalogStart = frames.length; await page.reload(); await expect(page.locator('[data-startup-phase="visible"]')).toBeVisible(); await expect(input).toBeEnabled();
    await catalogAcceptance(input, 'skills.list', 'existing-session');
    await open(input);
    await expect(options.first()).toHaveAttribute('aria-selected', 'true');
    await input.press('ArrowDown');
    await expect(options.nth(1)).toHaveAttribute('aria-selected', 'true');
    await input.press('ArrowUp');
    await expect(options.first()).toHaveAttribute('aria-selected', 'true');
    await input.press('Enter');
    await expect(input).toHaveValue('$accept-alpha ');
    await expect(list).toHaveCount(0);
    await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', repeat: true, bubbles: true });
    await expect(input).toHaveValue('$accept-alpha ');
    assert.equal(sends().length, sendCount, 'Held Enter after selection must not submit');
    const workspaceRequest = frames.filter(frame => frame.method === 'skills.list').at(-1);
    assert.equal(workspaceRequest.params.session_id, root);
    assert.equal(workspaceRequest.params.limit, 100);
    assert.equal(workspaceRequest.params.prefix, '');
    await open(input);
    await options.nth(1).click();
    await expect(input).toHaveValue('$accept-alpine ');
    await expect(input).toBeFocused();
    await open(input);
    await input.press('Escape');
    await expect(list).toHaveCount(0);
    await expect(input).toHaveValue('/accept-al');
    await expect(input).toBeFocused();
    checks.push('existing-session catalog uses exact session owner and bounded keyset pages; Up/Down/Enter/click select and Escape preserves draft/focus');

    await input.fill('Before /accept-al after');
    await input.evaluate(element => element.setSelectionRange(18, 18));
    await input.press('ArrowLeft'); // Native selection event: caret at the slash token end.
    await expect(options).toHaveCount(2);
    await input.press('Enter');
    await expect(input).toHaveValue('Before $accept-alpha after');
    await expect.poll(() => input.evaluate(element => element.selectionStart)).toBe('Before $accept-alpha '.length);
    checks.push('caret-middle replacement preserves surrounding draft and restores insertion caret');

    for (const theme of ['light', 'dark']) {
      await page.evaluate(theme => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme })), theme);
      await page.reload();
      await expect(input).toBeEnabled();
      await open(input, '/accept-long-');
      await screenshot(theme);
    }
    const reading = page.getByRole('region', { name: 'Conversation', exact: true });
    const before = await reading.evaluate(element => ({ top: element.scrollTop, height: element.clientHeight }));
    for (let i = 0; i < 24; i++) await input.press('ArrowDown');
    await expect.poll(() => list.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
    const after = await reading.evaluate(element => ({ top: element.scrollTop, height: element.clientHeight }));
    assert.deepEqual(after, before, 'Scrolling active suggestion must not scroll/resize conversation');
    checks.push('bounded 32-item long list scrolls independently of conversation');
    await page.setViewportSize({ width: 390, height: 844 });
    await input.press('Escape');
    await open(input);
    await expect(list).toBeInViewport();
    const bounds = await list.boundingBox();
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 391, 'Narrow popup stays within viewport');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await screenshot('narrow');
    await input.press('Escape');
    assert.equal(sends().length, sendCount, 'Picker interactions never submit');
    assert.deepEqual(await fixture.effects(), settledEffects, 'Picker interactions never invoke a model');
    assert.deepEqual(csp, []); assert.deepEqual(proxy.errors, []);
    assert.deepEqual(errors, []);
    checks.push('light/dark/narrow screenshots; no document overflow, page/CSP errors or extra model requests');
    report.status = 'passed'; report.metadataBytes = proxy.bytes;
    console.log(`${name}: ${checks.length} native slash skill workflows passed`);
  } catch (error) {
    report.status = 'failed'; report.failure = String(error.stack ?? error);
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), report.failure + '\n' + (await page?.locator('body').innerText().catch(() => '') ?? '').slice(0, 65536));
    process.exitCode = 1; console.error(error);
  } finally {
    proxy?.close();
    try { await browser?.close(); } finally {
      try { await fixture?.close(); } finally {
        try { proxy?.assertBounds(); assert.equal(report.overflow, undefined); }
        finally { await writeFile(join(directory, `${name}-frames.json`), JSON.stringify({ frames: proxy?.frames, responses: proxy?.responses }, null, 2)); await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); }
      }
    }
  }
  if (report.status === 'failed') break;
}
assert(reports.length && reports.every(report => report.status === 'passed'), 'Every selected browser must run and pass');
console.log(JSON.stringify(reports, null, 2));
