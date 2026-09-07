import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {mkdtemp, mkdir, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {resolve} from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {createRequire} from 'node:module';
import {chromium} from '@playwright/test';

const exec = promisify(execFile);
const repo = fileURLToPath(new URL('../../../', import.meta.url));
const directory = await mkdtemp(resolve(tmpdir(), 'whip-packed-ui-'));
const artifacts = resolve(directory, 'archives');
const consumer = resolve(directory, 'consumer');
await mkdir(artifacts); await mkdir(consumer);
try {
  const archives = {};
  for (const name of ['protocol', 'sdk', 'ui', 'app']) {
    const {stdout} = await exec('npm', ['pack', '--json', '--pack-destination', artifacts], {cwd: resolve(repo, `packages/${name}`), maxBuffer: 1024 * 1024});
    const [archive] = JSON.parse(stdout);
    archives[`@whip/${name}`] = `file:${resolve(artifacts, archive.filename)}`;
  }
  await writeFile(resolve(consumer, 'package.json'), JSON.stringify({name: 'whip-packed-consumer', private: true, type: 'module', dependencies: {...archives, react: '19.2.8', 'react-dom': '19.2.8', vite: '8.2.2', '@vitejs/plugin-react': '6.1.1', '@stylexjs/unplugin': '0.19.0'}}, null, 2));
  await exec('npm', ['install', '--ignore-scripts', '--no-audit', '--no-fund'], {cwd: consumer, maxBuffer: 1024 * 1024});
  await writeFile(resolve(consumer, 'index.html'), '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Packed WHIP consumer</title></head><body><div id="root"></div><script type="module" src="/main.tsx"></script></body></html>');
  await writeFile(resolve(consumer, 'main.tsx'), `import {createRoot} from 'react-dom/client';
import {createWhipApplication} from '@whip/app';
import {ThemeProvider, UIProvider, Button, CodeBlock} from '@whip/ui';
import {WorkspaceTabs,workspaceTabId} from '@whip/ui/workspace-tabs';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
const values = new Map();
const application = createWhipApplication({storage:{keys:()=>[...values.keys()],getItem:key=>values.get(key)??null,setItem:(key,value)=>values.set(key,value),removeItem:key=>values.delete(key)},defaultEndpoint:'http://127.0.0.1:1',openExternal(){},async copy(){},download(){}});
createRoot(document.getElementById('root')).render(new URLSearchParams(location.search).has('app') ? <application.Application/> : <ThemeProvider initialTheme="dark"><UIProvider><main><h1>Packed UI consumer</h1><WorkspaceTabs value="packed" items={[{value:'packed',label:'Packed tab',render:<a href="#packed-panel"/>}]} onClose={()=>{}} panelId="packed-panel"/><section id="packed-panel" role="tabpanel" aria-labelledby={workspaceTabId('packed')}><Button>Package button</Button><CodeBlock language="starlark" code="return True"/></section></main></UIProvider></ThemeProvider>);
window.addEventListener('pagehide',()=>application.dispose());
`);
  // The official StyleX plugin discovers source packages from the consumer cwd.
  process.chdir(consumer);
  const require = createRequire(resolve(consumer, 'package.json'));
  const {build, preview, createServer} = await import(pathToFileURL(require.resolve('vite')).href);
  const {default: react} = await import(pathToFileURL(require.resolve('@vitejs/plugin-react')).href);
  const {default: stylex} = await import(pathToFileURL(require.resolve('@stylexjs/unplugin')).href);
  const config = () => ({configFile: false, root: consumer, plugins: [stylex.vite({useCSSLayers: true, runtimeInjection: false, unstable_moduleResolution: {type: 'commonJS', rootDir: consumer}}), react()], logLevel: 'warn', optimizeDeps: {include: ['use-sync-external-store/shim', 'use-sync-external-store/shim/with-selector']}, build: {assetsInlineLimit: 0}, server: {host: '127.0.0.1', port: 0}, preview: {host: '127.0.0.1', port: 0}});
  await build(config());
  const browser = await chromium.launch(); const results = [];
  try {
    for (const mode of ['production', 'development']) {
      const server = mode === 'production' ? await preview(config()) : await createServer(config());
      if (mode === 'development') await server.listen();
      const port = server.httpServer.address().port;
      try {
        for (const target of ['ui', 'app']) {
          const page = await browser.newPage(); const errors = [];
          page.on('pageerror', error => errors.push(error.message));
          await page.goto(`http://127.0.0.1:${port}/${target === 'app' ? '?app=1' : ''}`);
          if (target === 'ui') {
            const button = page.getByRole('button', {name: 'Package button'});
            await button.waitFor(); await page.locator('figure[data-highlighted="true"]').waitFor();
            await page.getByRole('tab', {name:'Packed tab'}).waitFor();
            const styled = await button.evaluate(el => {const style = getComputedStyle(el); return style.borderRadius !== '0px' && style.display === 'inline-flex';});
            if (!styled) throw new Error(`${mode}: packed UI styles were not compiled`);
          } else {
            await page.getByRole('button', {name: 'Connect to host', exact: true}).first().waitFor();
            await page.getByRole('button', {name: 'Connect to host', exact: true}).first().click();
            await page.getByRole('dialog', {name: 'Connect to an execution host'}).waitFor();
          }
          if (errors.length) throw new Error(`${mode}/${target}: ${errors.join('; ')}`);
          results.push({mode, target, passed: true}); await page.close();
        }
      } finally {if (mode === 'development') await server.close(); else await new Promise(resolve => server.httpServer.close(resolve));}
    }
  } finally {await browser.close();}
  const output = resolve(repo, 'packages/ui/ui-test-results'); await mkdir(output, {recursive: true});
  await writeFile(resolve(output, 'packed-report.json'), JSON.stringify({node: process.version, packages: Object.keys(archives), results}, null, 2));
  console.log('Packed UI and app source packages passed isolated install, production rendering and Vite development rendering.');
} finally {process.chdir(repo); await rm(directory, {recursive: true, force: true});}
