import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useForm } from '@tanstack/react-form';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type {
  ConfigurationUpdate,
  ProviderLoginStatus,
  Resolved,
  RuntimeConfiguration,
} from '@whip/protocol';
import {
  Badge,
  Button,
  Combobox,
  Dialog,
  Field,
  Input,
  Select,
  Switch,
  Tabs,
  ThemePicker,
  useTheme,
} from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { commandShortcuts, composerShortcuts } from './runtime';
import { layout } from './styles';

export function Settings({ section = 'appearance' }: { section?: string }) {
  const { client } = useAppState();
  const navigate = useNavigate();
  return (
    <div {...stylex.props(layout.page)}>
      <div {...stylex.props(layout.pageInner)}>
        <h1 {...stylex.props(layout.pageTitle)}>Settings</h1>
        <Tabs
          value={section}
          onValueChange={(section) =>
            void navigate({
              to: '/settings',
              search: { section },
              replace: true,
            })
          }
          items={[
            'appearance',
            'providers',
            'runtime',
            'device',
            'recovery',
          ].map((value) => ({
            value,
            label: value[0]!.toUpperCase() + value.slice(1),
            content:
              value === 'appearance' ? (
                <Appearance client={client} />
              ) : value === 'device' ? (
                <DeviceSettings />
              ) : client ? (
                <HostSettings client={client} section={value} />
              ) : (
                <p>Connect to an execution host to configure it.</p>
              ),
          }))}
        />
        <Link to="/">Back to sessions</Link>
      </div>
    </div>
  );
}

function DeviceSettings() {
  const runtime = useRuntime();
  const { preferences } = useAppState();
  function update(patch: Parameters<typeof runtime.setPreferences>[0]) {
    try {
      runtime.setPreferences(patch);
    } catch (error) {
      runtime.report(error);
    }
  }
  return (
    <div {...stylex.props(layout.column)}>
      <h2>Keyboard and attention</h2>
      <p {...stylex.props(layout.muted)}>
        These preferences belong to this device. Mod means Command on macOS and
        Control elsewhere.
      </p>
      <Select
        label="Open commands"
        value={preferences.commandShortcut}
        options={commandShortcuts.map((value) => ({ value, label: value }))}
        onValueChange={(value) =>
          update({
            commandShortcut: value as typeof preferences.commandShortcut,
          })
        }
      />
      <Select
        label="Focus message composer"
        value={preferences.composerShortcut}
        options={composerShortcuts.map((value) => ({ value, label: value }))}
        onValueChange={(value) =>
          update({
            composerShortcut: value as typeof preferences.composerShortcut,
          })
        }
      />
      <Switch
        label="Announce attention changes"
        description="Politely announce sessions needing a response to assistive technology. The visual attention indicator always remains available."
        checked={preferences.attentionAnnouncements}
        onCheckedChange={(attentionAnnouncements) =>
          update({ attentionAnnouncements })
        }
      />
      <p>
        Enter sends a message. Shift + Enter inserts a line. Escape closes a
        menu or dialog. Tool output and Starlark are read-only; interactive
        terminals are available in the TUI.
      </p>
    </div>
  );
}

export function themeFromHost(value: Resolved, namespace: string) {
  const { on_primary, border_focus, diff_add, diff_del, ...colors } =
    value.colors;
  return {
    ...value,
    id: `${namespace}:${value.id}`,
    web: value.web ? {
      ...(value.web.navigation ? {navigation: value.web.navigation} : {}),
      ...(value.web.quiet_border ? {quietBorder: value.web.quiet_border} : {}),
      ...(value.web.code_background ? {codeBackground: value.web.code_background} : {}),
      ...(value.web.inline_code_background ? {inlineCodeBackground: value.web.inline_code_background} : {}),
    } : undefined,
    colors: {
      ...colors,
      onPrimary: on_primary,
      borderFocus: border_focus,
      diffAdd: diff_add,
      diffDel: diff_del,
    },
  };
}
function Appearance({ client }: { client?: WhipClient }) {
  const theme = useTheme();
  return (
    <>
      <div {...stylex.props(layout.settingsRow)}>
        <div>
          <strong>Color theme</strong>
          <p {...stylex.props(layout.muted)}>
            All TUI themes, with accessible browser surfaces. Saved on this
            device.
          </p>
        </div>
        <ThemePicker />
      </div>
      <div {...stylex.props(layout.notice)}>
        <strong>{theme.resolvedTheme.name}</strong>
        <p>
          Themes apply to controls, dialogs, Markdown, and code. System
          appearance follows your operating system.
        </p>
      </div>
      {client && <CustomThemes client={client} />}
    </>
  );
}
function CustomThemes({ client }: { client: WhipClient }) {
  const runtime = useRuntime();
  const connection = useWhipConnection(client);
  const theme = useTheme();
  const [busy, setBusy] = useState(false);
  const enabled = connection.state === 'connected';
  const themes = useQuery({
    queryKey: ['host-themes', client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => client.host.themes.list({ signal }),
    enabled,
  });
  const custom =
    themes.data?.themes?.filter((item) => item.source !== 'builtin') ?? [];
  function install(resolved: Resolved, namespace: string) {
    const value = themeFromHost(resolved, namespace);
    theme.addTheme(value);
    theme.setTheme(value.id);
  }
  return (
    <>
      <h2>Custom themes</h2>
      <p {...stylex.props(layout.muted)}>
        Use a theme from the host’s themes directory, or import the same JSON
        format used by the TUI. Selection stays on this device.
      </p>
      {custom.length > 0 && (
        <Combobox
          label="Host theme"
          options={custom.map((item) => ({ value: item.id, label: item.name }))}
          onValueChange={(name) =>
            void client.host.themes
              .resolve(name)
              .then((value) =>
                install(value, `host:${client.getSnapshot().info?.runtime_id}`),
              )
              .catch((error) => runtime.report(error))
          }
        />
      )}
      <Field
        label="Import theme JSON"
        description="Maximum 64 KiB. The host validates and resolves the theme; it does not save the file."
      >
        <Input
          type="file"
          accept=".json,application/json"
          disabled={!enabled || busy}
          onChange={async (event) => {
            const file = event.target.files?.[0];
            event.target.value = '';
            if (!file) return;
            setBusy(true);
            try {
              if (file.size > 64 * 1024)
                throw new Error('Theme JSON must be at most 64 KiB.');
              const result = await client.host.themes.resolveJSON(
                await file.text(),
              );
              install(result, `import:${crypto.randomUUID()}`);
            } catch (error) {
              runtime.report(error);
            } finally {
              setBusy(false);
            }
          }}
        />
      </Field>
      {themes.error && <p role="alert">{themes.error.message}</p>}
      {themes.data?.errors?.map((error) => (
        <p key={error.file} role="status">
          {error.file}: {error.message}
        </p>
      ))}
      {themes.data?.truncated && <p>The host theme list was truncated.</p>}
    </>
  );
}

function HostSettings({
  client,
  section,
}: {
  client: WhipClient;
  section: string;
}) {
  const connection = useWhipConnection(client);
  const enabled = connection.state === 'connected';
  return (
    <>
      {!enabled && (
        <p role="status" {...stylex.props(layout.notice)}>
          Connect to the host to change runtime settings.
        </p>
      )}
      {section === 'providers' && (
        <ProvidersSettings client={client} enabled={enabled} />
      )}
      {section === 'runtime' && (
        <RuntimeSettings client={client} enabled={enabled} />
      )}
      {section === 'recovery' && <Recovery client={client} enabled={enabled} />}
    </>
  );
}

export function ProvidersSettings({
  client,
  enabled,
}: {
  client: WhipClient;
  enabled: boolean;
}) {
  const info = client.getSnapshot().info;
  return (
    <ProviderSettingsForm
      key={`${info?.runtime_id ?? ''}:${info?.connection_id ?? ''}`}
      client={client}
      enabled={enabled}
    />
  );
}
function ProviderSettingsForm({
  client,
  enabled,
}: {
  client: WhipClient;
  enabled: boolean;
}) {
  const runtime = useRuntime();
  const catalogs = useQuery({
    queryKey: ['provider-catalogs', client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => client.providers.catalogs({ signal }),
    enabled,
    gcTime: 0,
  });
  const flows = useQuery({
    queryKey: ['provider-login-flows', client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => client.providers.login.list({ signal }),
    enabled,
    gcTime: 0,
    refetchInterval: enabled ? 2000 : false,
  });
  const [provider, setProvider] = useState('openrouter');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [validation, setValidation] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const baseURL = catalogs.data?.result?.providers[provider]?.base_url;
  const canSetKey = provider === 'openrouter' || provider === 'inference';
  async function validateKey() {
    if (!enabled || busy || !baseURL || !key.trim()) return;
    setBusy(true);
    setValidation('');
    const secret = key;
    setKey('');
    const controller = new AbortController();
    request.current = controller;
    try {
      const result = await client.providers.validate(
        { name: provider, base_url: baseURL, key: secret },
        { signal: controller.signal },
      );
      setValidation(
        `Validation succeeded: ${result.models?.length ?? 0} models available. The key was not saved.`,
      );
    } catch (error) {
      if (!controller.signal.aborted) runtime.report(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  async function setKeyOnHost(environment: boolean) {
    if (!enabled || busy) return;
    setBusy(true);
    const secret = key;
    setKey('');
    setValidation('');
    const controller = new AbortController();
    request.current = controller;
    try {
      const config = await client.configuration.get({
        signal: controller.signal,
      });
      await client.providers.setKey(
        {
          revision: config.revision,
          provider,
          ...(environment ? {} : { key: secret }),
          environment,
        },
        { signal: controller.signal },
      );
      await runtime.queries.invalidateQueries();
    } catch (error) {
      if (!controller.signal.aborted) runtime.report(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <>
      <h2>Model providers</h2>
      <p {...stylex.props(layout.muted)}>
        Credentials are managed by the execution host. API keys are sent once
        and never stored in drafts, recovery records, or query caches.
      </p>
      <Button
        disabled={!enabled || busy}
        onClick={async () => {
          setBusy(true);
          try {
            await client.providers.login.begin();
            await flows.refetch();
          } catch (error) {
            runtime.report(error);
          } finally {
            setBusy(false);
          }
        }}
      >
        Sign in with Inference.net
      </Button>
      {flows.data?.flows?.map((flow) => (
        <LoginFlow
          key={flow.flow_id}
          flow={flow}
          client={client}
          enabled={enabled}
          refresh={() => {
            void flows.refetch();
          }}
        />
      ))}
      {flows.error && <p role="alert">{flows.error.message}</p>}
      <Field label="Provider">
        <Select
          label="Provider"
          value={provider}
          onValueChange={(value) => {
            setKey('');
            setValidation('');
            setProvider(value);
          }}
          options={[
            ...new Set([
              'openrouter',
              'inference',
              ...Object.keys(catalogs.data?.result?.providers ?? {}),
            ]),
          ].map((value) => ({ value, label: value }))}
        />
      </Field>
      <ProviderState
        key={provider}
        client={client}
        provider={provider}
        enabled={enabled}
      />
      <Field
        label="API key"
        description="Saving validates the key on the execution host. Validate without saving only checks access; it does not change configuration."
      >
        <Input
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={key}
          onChange={(event) => setKey(event.target.value)}
        />
      </Field>
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button
          disabled={!enabled || busy || !key.trim() || !canSetKey}
          onClick={() => void setKeyOnHost(false)}
        >
          Save key on host
        </Button>
        <Button
          variant="secondary"
          disabled={!enabled || busy || !canSetKey}
          onClick={() => void setKeyOnHost(true)}
        >
          Use host environment
        </Button>
        <Button
          variant="secondary"
          disabled={!enabled || busy || !key.trim() || !baseURL}
          onClick={() => void validateKey()}
        >
          Validate without saving
        </Button>
      </div>
      {validation && <p role="status">{validation}</p>}
      {!canSetKey && (
        <p role="status">
          This host does not offer credential setup for this provider.
        </p>
      )}
      {catalogs.error && <p role="alert">{catalogs.error.message}</p>}
    </>
  );
}
export function ProviderState({
  client,
  provider,
  enabled,
}: {
  client: WhipClient;
  provider: string;
  enabled: boolean;
}) {
  const runtime = useRuntime();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const state = useQuery({
    queryKey: [
      'provider-status',
      client.getSnapshot().info?.runtime_id,
      provider,
    ],
    queryFn: ({ signal }) => client.providers.status(provider, { signal }),
    enabled,
    gcTime: 0,
  });
  async function accountAction(rotate: boolean) {
    if (!enabled || busy) return;
    setBusy(true);
    setNotice('');
    const controller = new AbortController();
    request.current = controller;
    try {
      if (rotate)
        await client.providers.rotateKey(provider, {
          signal: controller.signal,
        });
      else
        await client.providers.logout(provider, { signal: controller.signal });
      setNotice(
        rotate
          ? 'Machine key rotated on the execution host.'
          : 'Provider disconnected.',
      );
      await runtime.queries.invalidateQueries();
    } catch (error) {
      if (!controller.signal.aborted) {
        runtime.report(error);
        setNotice(
          'Inspect the current account status before retrying an interrupted account change.',
        );
        await state.refetch();
      }
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <span>
        <Badge tone={state.data?.configured ? 'success' : 'neutral'}>
          {state.data?.configured ? 'Connected' : 'Not configured'}
        </Badge>{' '}
        {state.data?.email} {state.data?.project_name}
      </span>
      {state.data?.warnings?.map((message) => (
        <p key={message}>{message}</p>
      ))}
      {state.error && <p role="alert">{state.error.message}</p>}
      {state.data?.configured && provider === 'inference' && (
        <>
          {state.data.machine_key_name && (
            <span>Machine key: {state.data.machine_key_name}</span>
          )}
          <Button
            variant="secondary"
            disabled={!enabled || busy || !state.data.email}
            onClick={() => void accountAction(true)}
          >
            Rotate machine key
          </Button>
          <Button
            variant="ghost"
            disabled={!enabled || busy}
            onClick={() => void accountAction(false)}
          >
            Disconnect provider
          </Button>
        </>
      )}
      {notice && <p role="status">{notice}</p>}
    </div>
  );
}
function LoginFlow({
  flow,
  client,
  enabled,
  refresh,
}: {
  flow: ProviderLoginStatus;
  client: WhipClient;
  enabled: boolean;
  refresh(): void;
}) {
  const runtime = useRuntime();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const action = async (run: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await run();
      refresh();
    } catch (error) {
      runtime.report(error);
    } finally {
      setBusy(false);
    }
  };
  const terminal = [
    'succeeded',
    'failed',
    'cancelled',
    'interrupted',
    'expired',
  ].includes(flow.state);
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <div {...stylex.props(layout.row)}>
        <Badge>{flow.state}</Badge>
        <span>{flow.email}</span>
      </div>
      {flow.verification_url && !terminal && (
        <>
          <Button
            variant="secondary"
            onClick={() =>
              runtime.platform.openExternal(flow.verification_url!)
            }
          >
            Open verification page
          </Button>
          <span>
            Verification code: <strong>{flow.user_code}</strong>
          </span>
        </>
      )}
      {!!flow.teams?.length && !flow.team_id && (
        <Select
          label="Workspace"
          disabled={!enabled || busy}
          options={flow.teams.map((team) => ({
            value: team.id,
            label: team.name,
          }))}
          onValueChange={(id) =>
            void action(() =>
              client.providers.login.selectTeam(flow.flow_id, id),
            )
          }
        />
      )}
      {!!flow.projects?.length && flow.team_id && !flow.project_id && (
        <Select
          label="Project"
          disabled={!enabled || busy}
          options={flow.projects.map((project) => ({
            value: project.id,
            label: project.name,
          }))}
          onValueChange={(id) =>
            void action(() =>
              client.providers.login.selectProject(flow.flow_id, id),
            )
          }
        />
      )}
      {flow.team_id && !flow.project_id && !terminal && (
        <>
          <Field label="New project name">
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <Button
            variant="ghost"
            disabled={!enabled || busy || !name.trim()}
            onClick={() =>
              void action(() =>
                client.providers.login.createProject(flow.flow_id, name.trim()),
              )
            }
          >
            Create project
          </Button>
        </>
      )}
      {flow.error && <p role="alert">{flow.error}</p>}
      {!terminal && (
        <Button
          variant="ghost"
          disabled={!enabled || busy}
          onClick={() =>
            void action(() => client.providers.login.cancel(flow.flow_id))
          }
        >
          Cancel sign-in
        </Button>
      )}
    </div>
  );
}

function RuntimeSettings({
  client,
  enabled,
}: {
  client: WhipClient;
  enabled: boolean;
}) {
  const [editor, setEditor] = useState(0);
  const configuration = useQuery({
    queryKey: ['runtime-configuration', client.getSnapshot().info?.runtime_id],
    queryFn: ({ signal }) => client.configuration.get({ signal }),
    enabled,
  });
  return (
    <>
      {configuration.error && <p role="alert">{configuration.error.message}</p>}
      {configuration.data && (
        <ConfigurationForm
          key={editor}
          client={client}
          config={configuration.data}
          enabled={enabled}
          reload={() => setEditor((value) => value + 1)}
        />
      )}
    </>
  );
}
function ConfigurationForm({
  client,
  config,
  enabled,
  reload,
}: {
  client: WhipClient;
  config: RuntimeConfiguration;
  enabled: boolean;
  reload(): void;
}) {
  const runtime = useRuntime();
  const [error, setError] = useState('');
  const [base] = useState(config);
  const form = useForm({
    defaultValues: {
      default_model: base.default_model,
      default_provider: base.default_provider,
      default_effort: base.default_effort,
      compact_model: base.compact_model,
      compact_provider: base.compact_provider,
      compact_percent: base.compact_percent,
      goal_max_rounds: base.goal_max_rounds,
      max_retries: base.max_retries,
      import_claude: base.import_claude,
      import_codex: base.import_codex,
    },
    onSubmit: async ({ value }) => {
      setError('');
      try {
        const patch: ConfigurationUpdate = {
          ...value,
          revision: base.revision,
        };
        await client.configuration.update(patch);
        await runtime.queries.invalidateQueries();
        reload();
      } catch (error) {
        setError(error instanceof Error ? error.message : String(error));
      }
    },
  });
  return (
    <form
      {...stylex.props(layout.column)}
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <h2>Execution host defaults</h2>
      <p {...stylex.props(layout.muted)}>
        These defaults are shared with other clients. Changes use revision{' '}
        {base.revision}; conflicts require a fresh review.
      </p>
      {base.revision !== config.revision && (
        <div role="status" {...stylex.props(layout.notice)}>
          The host configuration changed. Your edits are preserved.
          <Button type="button" variant="secondary" onClick={reload}>
            Discard edits and load current defaults
          </Button>
        </div>
      )}
      {(
        [
          'default_model',
          'default_provider',
          'default_effort',
          'compact_model',
          'compact_provider',
        ] as const
      ).map((name) => (
        <form.Field key={name} name={name}>
          {(field) => (
            <Field label={name.replaceAll('_', ' ')}>
              <Input
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
      ))}
      {(['compact_percent', 'goal_max_rounds', 'max_retries'] as const).map(
        (name) => (
          <form.Field key={name} name={name}>
            {(field) => (
              <Field label={name.replaceAll('_', ' ')}>
                <Input
                  type="number"
                  min={0}
                  step={1}
                  value={field.state.value}
                  onChange={(event) =>
                    field.handleChange(event.target.valueAsNumber)
                  }
                />
              </Field>
            )}
          </form.Field>
        ),
      )}
      {(['import_claude', 'import_codex'] as const).map((name) => (
        <form.Field key={name} name={name}>
          {(field) => (
            <Switch
              label={name.replaceAll('_', ' ')}
              checked={field.state.value}
              onCheckedChange={field.handleChange}
            />
          )}
        </form.Field>
      ))}
      {error && <p role="alert">{error}</p>}
      <form.Subscribe selector={(state) => state.isSubmitting}>
        {(submitting) => (
          <Button type="submit" disabled={!enabled || submitting}>
            Save host defaults
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}
function Recovery({
  client,
  enabled,
}: {
  client: WhipClient;
  enabled: boolean;
}) {
  const runtime = useRuntime();
  const { commands } = useAppState();
  const records = useQuery({
    queryKey: ['command-recovery', client.getSnapshot().info?.runtime_id],
    queryFn: () => client.recoveryRecords(),
  });
  const [outcomes, setOutcomes] = useState<Record<string, string>>({});
  const [discard, setDiscard] = useState(false);
  return (
    <>
      <h2>Saved drafts</h2>
      <p {...stylex.props(layout.muted)}>Up to 32 unsent drafts are stored in this browser. Discard drafts here to reclaim space, including drafts from deleted or inaccessible sessions.</p>
      <Button variant="secondary" onClick={() => setDiscard(true)}>Discard saved drafts…</Button>
      <Dialog open={discard} onOpenChange={setDiscard} title="Discard saved drafts?" description="This removes unsent draft text stored by this browser. Accepted commands keep running. Other open tabs may still have unsaved edits." footer={<Button variant="danger" onClick={() => { try { runtime.discardDrafts(); setDiscard(false); } catch(error) { runtime.report(error); } }}>Discard drafts</Button>} />
      <h2>Command recovery</h2>
      <p {...stylex.props(layout.muted)}>
        A lost connection does not cancel accepted work. Check an uncertain
        command before submitting it again. Stored records contain identities,
        never prompt bodies.
      </p>
      {commands.map((command) => (
        <div key={command.id} {...stylex.props(layout.notice)}>
          {command.label} · {command.status}
          {command.error && <p>{command.error}</p>}
        </div>
      ))}
      {records.data?.map((record) => (
        <div
          key={`${record.runtimeId}:${record.commandId}`}
          {...stylex.props(layout.notice, layout.column)}
        >
          <strong>{record.operation}</strong>
          <span {...stylex.props(layout.muted)}>{record.commandId}</span>
          <span>{outcomes[record.commandId]}</span>
          <div {...stylex.props(layout.row)}>
            <Button
              variant="secondary"
              disabled={
                !enabled ||
                record.runtimeId !== client.getSnapshot().info?.runtime_id
              }
              onClick={async () => {
                try { const outcome = await client.recover(record).status(); setOutcomes(previous => ({ ...previous, [record.commandId]: outcome.status })); }
                catch (error) { setOutcomes(previous => ({ ...previous, [record.commandId]: error instanceof Error ? error.message : String(error) })); }
              }}
            >
              Check status
            </Button>
            <Button
              variant="ghost"
              onClick={() =>
                void client
                  .forget(record)
                  .then(() => records.refetch())
                  .catch((error) => runtime.report(error))
              }
            >
              Forget record
            </Button>
          </div>
        </div>
      ))}
      {records.error && <p role="alert">{records.error.message}</p>}
    </>
  );
}
