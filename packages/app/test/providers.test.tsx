import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import type { WhipClient } from '@whip/sdk';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { ProvidersSettings, ProviderState } from '../src/settings';

beforeEach(() =>
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })),
);
afterEach(() => vi.unstubAllGlobals());

function fixture() {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const client = {
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host' } }),
    providers: {
      catalogs: vi.fn(async () => ({
        result: {
          models: {},
          providers: { openrouter: { base_url: 'https://provider.example/v1' } },
        },
      })),
      status: vi.fn(async () => ({
        provider: 'inference',
        configured: true,
        email: 'user@example.com',
        key_source: 'machine',
        machine_key_name: 'work-mac',
        warnings: [],
      })),
      setKey: vi.fn(async () => ({})),
      validate: vi.fn(async (_params: unknown, _options: { signal: AbortSignal }) => ({
        models: [{ id: 'model' }],
      })),
      rotateKey: vi.fn(async () => ({})),
      logout: vi.fn(async () => ({})),
      login: { list: vi.fn(async () => ({ flows: [] })), begin: vi.fn(async () => ({})) },
    },
    configuration: { get: vi.fn(async () => ({ revision: '7' })) },
  };
  const report = vi.fn();
  const runtime = { queries, report } as unknown as AppRuntime;
  const typed = client as unknown as WhipClient;
  const wrap = (node: ReactNode) => (
    <RuntimeContext.Provider value={runtime}>
      <ThemeProvider initialTheme="light">
        <UIProvider>
          <QueryClientProvider client={queries}>{node}</QueryClientProvider>
        </UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>
  );
  return {
    client,
    typed,
    report,
    queries,
    render(node: ReactNode = <ProvidersSettings client={typed} enabled />) {
      const result = render(wrap(node));
      return {
        ...result,
        rerender(node: ReactNode) {
          result.rerender(wrap(node));
        },
      };
    },
  };
}

it('validates a key once without saving or retaining it in a query cache', async () => {
  const f = fixture();
  f.render();
  fireEvent.change(screen.getByLabelText('API key'), {
    target: { value: 'private-validation-key' },
  });
  const button = screen.getByRole('button', {
    name: 'Validate without saving',
  }) as HTMLButtonElement;
  await waitFor(() => expect(button.disabled).toBe(false));
  fireEvent.click(button);
  await screen.findByText('Validation succeeded: 1 models available. The key was not saved.');
  expect(f.client.providers.validate).toHaveBeenCalledExactlyOnceWith(
    { name: 'openrouter', base_url: 'https://provider.example/v1', key: 'private-validation-key' },
    { signal: expect.any(AbortSignal) },
  );
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
  expect(f.client.providers.setKey).not.toHaveBeenCalled();
  expect(
    JSON.stringify(
      f.queries
        .getQueryCache()
        .getAll()
        .map((query) => ({ key: query.queryKey, data: query.state.data })),
    ),
  ).not.toContain('private-validation-key');
});

it('saves a key through the host service that already validates it', async () => {
  const f = fixture();
  f.render();
  fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'private-setup-key' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save key on host' }));
  await waitFor(() =>
    expect(f.client.providers.setKey).toHaveBeenCalledExactlyOnceWith(
      { revision: '7', provider: 'openrouter', key: 'private-setup-key', environment: false },
      { signal: expect.any(AbortSignal) },
    ),
  );
  expect(f.client.providers.validate).not.toHaveBeenCalled();
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
});

it('rotates a host-owned Inference machine key and refreshes account status', async () => {
  const f = fixture();
  f.render(<ProviderState client={f.typed} provider="inference" enabled />);
  fireEvent.click(await screen.findByRole('button', { name: 'Rotate machine key' }));
  await screen.findByText('Machine key rotated on the execution host.');
  expect(f.client.providers.rotateKey).toHaveBeenCalledExactlyOnceWith('inference', {
    signal: expect.any(AbortSignal),
  });
  await waitFor(() => expect(f.client.providers.status.mock.calls.length).toBeGreaterThan(1));
  expect(f.client.providers.logout).not.toHaveBeenCalled();
});

it('reads current account state after an uncertain rotation without replaying it', async () => {
  const f = fixture();
  f.client.providers.rotateKey.mockRejectedValueOnce(new Error('acknowledgement lost'));
  f.render(<ProviderState client={f.typed} provider="inference" enabled />);
  fireEvent.click(await screen.findByRole('button', { name: 'Rotate machine key' }));
  await screen.findByText(
    'Inspect the current account status before retrying an interrupted account change.',
  );
  await waitFor(() => expect(f.client.providers.status.mock.calls.length).toBeGreaterThan(1));
  expect(f.client.providers.rotateKey).toHaveBeenCalledTimes(1);
  expect(f.report).toHaveBeenCalledWith(
    expect.objectContaining({ message: 'acknowledgement lost' }),
  );
});

it('does not offer machine account operations for an API-key provider', async () => {
  const f = fixture();
  f.render(<ProviderState client={f.typed} provider="openrouter" enabled />);
  await screen.findByText('Connected');
  expect(screen.queryByRole('button', { name: 'Rotate machine key' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Disconnect provider' })).toBeNull();
});

it('aborts a local provider validation wait when settings close', async () => {
  const f = fixture();
  f.client.providers.validate.mockImplementationOnce(() => new Promise(() => {}));
  const mounted = f.render();
  fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'temporary' } });
  const button = screen.getByRole('button', {
    name: 'Validate without saving',
  }) as HTMLButtonElement;
  await waitFor(() => expect(button.disabled).toBe(false));
  fireEvent.click(button);
  await waitFor(() => expect(f.client.providers.validate).toHaveBeenCalledTimes(1));
  const signal = f.client.providers.validate.mock.calls[0]![1].signal;
  expect(signal.aborted).toBe(false);
  mounted.unmount();
  expect(signal.aborted).toBe(true);
});

it('discards typed secrets and aborts waits when the execution host changes', async () => {
  const f = fixture();
  const other = fixture();
  other.client.getSnapshot = () => ({ state: 'connected', info: { runtime_id: 'other-host' } });
  const mounted = f.render();
  fireEvent.change(screen.getByLabelText('API key'), {
    target: { value: 'intended-for-first-host' },
  });
  mounted.rerender(<ProvidersSettings client={other.typed} enabled />);
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
  fireEvent.click(screen.getByRole('button', { name: 'Save key on host' }));
  expect(other.client.providers.setKey).not.toHaveBeenCalled();

  other.client.providers.validate.mockImplementationOnce(() => new Promise(() => {}));
  fireEvent.change(screen.getByLabelText('API key'), {
    target: { value: 'temporary-second-host-key' },
  });
  const button = screen.getByRole('button', {
    name: 'Validate without saving',
  }) as HTMLButtonElement;
  await waitFor(() => expect(button.disabled).toBe(false));
  fireEvent.click(button);
  await waitFor(() => expect(other.client.providers.validate).toHaveBeenCalledTimes(1));
  const signal = other.client.providers.validate.mock.calls[0]![1].signal;
  mounted.rerender(<ProvidersSettings client={f.typed} enabled />);
  expect(signal.aborted).toBe(true);
  expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('');
});
