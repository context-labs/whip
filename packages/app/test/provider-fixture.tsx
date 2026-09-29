import type { ReactNode } from 'react';
import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Client, type Request, type ProviderInventory, type ProviderModelsResult, type ProviderPresetsResult, type InferenceFlow, type OpenAILoginFlow } from '@whip/sdk';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { vi } from 'vitest';
import { unknownPrices } from '../src/model-options';
export const revision = 'a'.repeat(64);
export const nextRevision = 'b'.repeat(64);
export const model = (id = 'fixture'): ProviderModelsResult['items'][number] => ({ id, name: id, prices: { ...unknownPrices }, context_window_tokens: null, advertised_context_tokens: null, effective_context_percent: null, max_output_tokens: null, reasoning_efforts: ['low', 'high'], input_modalities: ['text'], output_modalities: ['text'], supports_tools: true, metadata_source: 'bundled' });
export const route = (id = 'openrouter'): ProviderInventory['routes'][number] => ({ id, kind: 'openai-chat', base_url: 'https://example.test/v1', credential: { source: 'env', state: 'available', environment: 'OPENROUTER_API_KEY', file: '' }, models: {} });
export const preset = (id = 'openrouter'): ProviderPresetsResult['items'][number] => ({ id, name: id === 'openrouter' ? 'OpenRouter' : id, kind: id === 'openai-codex' ? 'openai-codex' : 'openai-chat', base_url: 'https://example.test/v1', methods: id === 'openai-codex' || id === 'inference-net' ? ['login'] : ['api_key'], environments: id === 'openrouter' ? ['OPENROUTER_API_KEY'] : [], key_url: '', suggested_models: ['fixture'], suggested_effort: '' });
export const inferenceFlow = (state: InferenceFlow['state'] = 'authorizing'): InferenceFlow => ({ id: `${'A'.repeat(26)}:${'B'.repeat(26)}`, kind: 'login', state, verification_url: state === 'authorizing' ? 'https://inference.net/device/approve?user_code=1234' : null, user_code: state === 'authorizing' ? '1234' : null, expires_at: null, teams: [], projects: [], team_id: null, project_id: null, failure: null });
export async function providerFixture() {
  const calls: Request[] = [];
  const data: { inventory: ProviderInventory; presets: ProviderPresetsResult; inference: InferenceFlow[]; openai: OpenAILoginFlow[]; handlers: Record<string, (request: Request, signal?: AbortSignal) => unknown | Promise<unknown>> } = {
    inventory: { revision, routes: [route()], defaults: { provider: 'openrouter', name: 'fixture', effort: '' }, compaction_model: null }, presets: { items: [preset(), preset('inference-net'), preset('openai-codex')] }, inference: [], openai: [], handlers: {},
  };
  const client = await Client.connect(async (request, _identity, options) => {
    options.signal?.throwIfAborted(); calls.push(structuredClone(request));
    let result: unknown;
    const handler = data.handlers[request.method];
    if (handler) result = await handler(request, options.signal);
    else switch (request.method) {
      case 'initialize': result = { major: 4, minor: 0, runtime_id: 'host', process_epoch: 'boot', network_client: false, builtins: [] }; break;
      case 'providers.list': result = data.inventory; break;
      case 'providers.presets': result = data.presets; break;
      case 'providers.readiness': result = { configured: true, credential_state: data.inventory.routes[0]?.credential.state ?? 'missing', catalog_state: 'missing', model_state: 'configured', inference_state: 'not_tested' }; break;
      case 'providers.bundled': result = { items: [model()] }; break;
      case 'providers.catalog': result = { provider: 'openrouter', state: 'missing', scope_state: 'unverified', discovery: 'not_checked', fetched_at: null, stale: false, failure: null, models: [] }; break;
      case 'accounts.inference.list': result = { items: data.inference }; break;
      case 'accounts.openai.list': result = { items: data.openai }; break;
      case 'accounts.inference.status': result = { management_state: 'stored', inference_state: 'stored', route_state: 'configured', user_id: 'user', email: 'test@example.test', expires_at: null, team_id: null, team_name: null, project_id: null, project_name: null, failure: null, cleanup_pending: false }; break;
      case 'accounts.openai.status': result = { auth_state: 'stored', route_state: 'configured', account_id: 'account', email: 'test@example.test', plan: 'Pro', expires_at: null, failure: null }; break;
      case 'accounts.inference.cleanup': result = { items: [], failure: null }; break;
      case 'host.permission_default': result = { revision: data.inventory.revision, mode: 'prompt' }; break;
      default: throw new Error(`Unexpected request ${request.method}`);
    }
    return { jsonrpc: '2.0', id: request.id, result: structuredClone(result) };
  }, { clientID: 'settings-fixture' });
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: Infinity } } });
  const storage = new Map<string, string>();
  const runtime = { queries, report: vi.fn(), recovery: { save: vi.fn() }, connections: { host: () => ({ name: 'Remote workstation' }) }, platform: { copy: vi.fn(async () => {}), openExternal: vi.fn(async () => {}), storage: { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value) } } } as unknown as AppRuntime;
  const wrap = (children: ReactNode) => <RuntimeContext.Provider value={runtime}><QueryClientProvider client={queries}><ThemeProvider initialTheme="light"><UIProvider>{children}</UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider>;
  return { client, data, calls, queries, runtime, storage, wrap, mount: (node: ReactNode) => render(wrap(node)), count: (method: string) => calls.filter(call => call.method === method).length };
}
export const sessionRecord = (id = 'child'): import('@whip/sdk').SessionRecord => ({
  history_revision: '1', id, tree_id: 'tree', parent_id: id === 'root' ? null : 'root', definition: { id: 'agent', revision }, config_revision: '9007199254740993', working_directory: '/fixture', lifecycle: 'active', created_at: '2026-09-28T00:00:00Z',
  configuration: { mcp_servers: { all: false, servers: [] }, modules: [], tools_definition: null, hooks_definition: null, automatic_title: false, goals_enabled: false, compaction: { model: null, threshold_percent: 50 }, report_mode: 'notice', model: { provider: 'openrouter', name: 'fixture', effort: 'low' }, instructions: { project_root: null, text: '', project_files: [], discover_skills: false, standing_instructions: false, skill_roots: [] }, tools: {}, children: {}, hooks: {}, output_schema: null },
});
