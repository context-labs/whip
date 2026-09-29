import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, CodeBlock, Field, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { mcpRefreshNotice } from '../mcp-refresh';
import { ExternalBrowser } from './external-browser';
import {
  Action,
  Empty,
  QueryFeedback,
  Section,
  useDetailQuery,
  type InspectorProps,
} from './shared';

export function Integrations(props: InspectorProps) {
  const [section, setSection] = useState('mcp');
  return (
    <>
      <Select
        label="Integration"
        value={section}
        onValueChange={setSection}
        options={[
          { value: 'mcp', label: 'MCP servers' },
          { value: 'lsp', label: 'Language servers' },
          { value: 'browser', label: 'Browser automation' },
          { value: 'computer', label: 'Computer policy' },
          { value: 'tools', label: 'Tools & host diagnostics' },
        ]}
      />
      {section === 'mcp' && <MCP {...props} />}
      {section === 'lsp' && <LSP {...props} />}
      {section === 'browser' && <Browser {...props} />}
      {section === 'computer' && <Computer {...props} />}
      {section === 'tools' && <Tools {...props} />}
    </>
  );
}
type MCPAction = 'reconnect' | 'enable' | 'disable';
const importSources = [
  { source: 'claude', label: 'Claude user file' },
  { source: 'codex', label: 'Codex configuration' },
  { source: 'project', label: 'Project .mcp.json' },
  { source: 'opencode', label: 'OpenCode configuration' },
] as const;
export function mcpActions(state: string): readonly MCPAction[] {
  switch (state) {
    case 'blocked':
    case 'unreadable':
      return [];
    case 'disabled':
      return ['enable'];
    case 'connecting':
      return ['disable'];
    default:
      return ['reconnect', 'disable'];
  }
}
export function MCP(props: InspectorProps) {
  const query = useDetailQuery(props, 'mcp.status', { session_id: props.session.id }, true),
    imports = useDetailQuery(props, 'mcp.configuration', {});
  const [notice, setNotice] = useState('');
  return (
    <>
      <Section
        title="MCP servers"
        description="Server processes, credentials, and authority belong to the execution host. Enable and disable affect this selected session only. Edit saved server declarations in Settings."
      >
        <div {...stylex.props(layout.row, layout.wrap)}>
          <Action
            disabled={!props.connected}
            run={async () => {
              setNotice('');
              setNotice(mcpRefreshNotice(await props.client.refreshMCP(props.session.id)));
              await query.refetch();
            }}
          >
            Refresh MCP configuration for this session
          </Action>
          <Action
            disabled={!props.connected}
            run={async () => {
              setNotice('');
              setNotice(mcpRefreshNotice(await props.client.reloadMCP(props.session.id)));
              await query.refetch();
            }}
          >
            Reload MCP connections
          </Action>
        </div>
        <p>
          Refresh adds new declarations. Reload retires existing connections and applies changed
          declarations; ongoing calls can be interrupted.
        </p>
        {notice && <p role="status">{notice}</p>}
        <QueryFeedback query={query} connected={props.connected} />
        {query.data?.items.length === 0 && (
          <Empty>No MCP servers are configured for this session.</Empty>
        )}
        {query.data?.items.map((server) => (
          <article key={server.name} {...stylex.props(layout.column, layout.notice)}>
            <div {...stylex.props(layout.row)}>
              <strong>{server.name}</strong>
              <Badge>{server.state}</Badge>
            </div>
            <span>
              {server.tools} tools · {server.source || 'Host configuration'}
            </span>
            {server.note && <p>{server.note}</p>}
            {server.failure && (
              <ErrorNotice type="resource" owner={`mcp:${server.name}`} error={server.failure} />
            )}
            <div {...stylex.props(layout.row, layout.wrap)}>
              {mcpActions(server.state).map((action) => (
                <Action
                  key={action}
                  disabled={!props.connected}
                  run={async () => {
                    await props.client.call(`mcp.${action}`, {
                      session_id: props.session.id,
                      server: server.name,
                    });
                    await query.refetch();
                  }}
                >
                  {action === 'reconnect'
                    ? `Reconnect ${server.name}`
                    : `${action === 'enable' ? 'Enable' : 'Disable'} ${server.name} for this session`}
                </Action>
              ))}
            </div>
          </article>
        ))}
      </Section>
      <Section
        title="Host import defaults"
        description="Discovery reads these external configurations on the host. Enabling a source does not itself grant permission to launch or call an untrusted server. Refresh or reload this session explicitly after saving."
      >
        <QueryFeedback query={imports} connected={props.connected} />
        {imports.data &&
          importSources.map(({ source, label }) => {
            const configuration = imports.data,
              policy = configuration.imports[source],
              enabled = policy?.enabled;
            return (
              <div key={source} {...stylex.props(layout.settingsRow)}>
                <span>
                  {label} ·{' '}
                  {enabled === null || enabled === undefined
                    ? 'Default'
                    : enabled
                      ? 'Enabled'
                      : 'Disabled'}
                </span>
                <Action
                  disabled={!props.connected}
                  run={async () => {
                    try {
                      await props.client.configureMCP({
                        revision: configuration.revision,
                        name: '',
                        server: null,
                        remove: false,
                        brand_icons: null,
                        imports: {
                          ...configuration.imports,
                          [source]: { only: [], exclude: [], ...policy, enabled: enabled !== true },
                        },
                      });
                    } finally {
                      await imports.refetch();
                    }
                  }}
                >
                  {enabled ? 'Disable' : 'Enable'} {source} imports
                </Action>
              </div>
            );
          })}
      </Section>
    </>
  );
}
function LSP(props: InspectorProps) {
  const query = useDetailQuery(props, 'lsp.status', { session_id: props.session.id }, true);
  return (
    <Section
      title="Language servers"
      description="Health and workspace roots on the execution machine. Reading status never starts a server."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {query.data?.items.length === 0 && <Empty>No language servers are active.</Empty>}
      {query.data?.items.map((server, index) => (
        <article
          key={`${server.name}:${server.workspace_root}:${index}`}
          {...stylex.props(layout.column, layout.notice)}
        >
          <strong>{server.name}</strong>
          <Badge>{server.state}</Badge>
          <code>{server.workspace_root}</code>
          {server.failure && (
            <ErrorNotice type="resource" owner={`lsp:${server.name}`} error={server.failure} />
          )}
        </article>
      ))}
    </Section>
  );
}
export function Browser(props: InspectorProps) {
  const query = useDetailQuery(
    props,
    'browser.attachments',
    { session_id: props.session.id },
    true,
  );
  return (
    <Section
      title="Browser automation"
      description="Exact page attachments authorized for this selected agent. Viewing these records does not select, attach, or control a browser."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {query.data?.attachments.length === 0 && <Empty>No attached browser pages.</Empty>}
      {query.data?.attachments.map((item) => (
        <article key={item.scope.attachment_id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{item.title || 'Untitled page'}</strong>
          <span>{item.url}</span>
          <span>Document revision {item.document_revision}</span>
          <details>
            <summary>Attachment identity</summary>
            <code>{item.scope.attachment_id}</code>
          </details>
        </article>
      ))}
      <Button variant="ghost" disabled={!props.connected} onClick={() => void query.refetch()}>
        Refresh attachments
      </Button>
      <BrowserDriver {...props} /><ExternalBrowser {...props} />
    </Section>
  );
}
function BrowserDriver(props: InspectorProps) {
  const query = useDetailQuery(props, 'host.browser_driver', {});
  const [draft, setDraft] = useState<{ value: 'rod' | 'chromedp'; revision: string }>();
  const [reviewRequired, setReviewRequired] = useState(false);
  const current = query.data;
  async function refresh() {
    const result = await query.refetch();
    if (!result.error) setReviewRequired(false);
  }
  return <>
    <h4>Host browser driver</h4>
    <QueryFeedback query={query} connected={props.connected} />
    {current && <>
      <Select label="Host browser driver" value={draft?.value ?? current.driver} disabled={current.pinned || !props.connected} options={[{ value: 'rod', label: 'Rod' }, { value: 'chromedp', label: 'ChromeDP' }]} onValueChange={value => { if (value === 'rod' || value === 'chromedp') setDraft({ value, revision: draft?.revision ?? current.revision }); }} />
      <p>This saved choice applies to future Desktop browser batches. Accepted Desktop work keeps its captured driver. A changed external Chrome driver retires its previous connections; new operations still require permission.</p>
      {current.pinned && <p>The running host pins {current.driver} through its startup environment.</p>}
      {draft && draft.revision !== current.revision && <p role="status">Host settings changed. This choice retains its original revision; discard it to use the current setting.</p>}
      <Action disabled={!props.connected || current.pinned || query.isFetching || reviewRequired || !draft} run={async () => {
        if (!draft) return;
        setReviewRequired(true);
        try { await props.client.hosts.setBrowserDriver(draft.revision, draft.value); setDraft(undefined); }
        finally { await refresh(); }
      }}>Save browser driver</Action>
      <Button variant="ghost" disabled={!props.connected || query.isFetching} onClick={() => { setDraft(undefined); void refresh(); }}>Discard choice and refresh driver</Button>
      {reviewRequired && <p role="status">Read the current host setting before another change. The previous request may have arrived.</p>}
    </>}
  </>;
}
export function Computer(props: InspectorProps) {
  const query = useDetailQuery(props, 'computer.status', {}),
    [app, setApp] = useState('');
  const [reviewRequired, setReviewRequired] = useState(false);
  async function refresh() {
    const result = await query.refetch();
    if (!result.error) setReviewRequired(false);
  }
  const status = query.data;
  return (
    <Section
      title="Computer app policy"
      description="These saved rules apply to the execution host. They do not change browser-client permissions. Configuration uses the exact host revision; a conflict is refreshed without replaying the edit."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {status && (
        <>
          <p>
            <Badge>{status.state}</Badge> · default{' '}
            {status.configuration.default_deny ? 'deny' : 'allow'}
          </p>
          {!status.platform_supported && (
            <p>Computer automation is unsupported on this host platform.</p>
          )}
          <p>Helper program: {status.configuration.helper_executable || 'None selected'}</p>
          <p>Selecting or enabling a helper saves configuration. Connect it separately when ready.</p>
          {status.bundled_available && <Action disabled={!props.connected || !!status.configuration.helper_executable || query.isFetching || reviewRequired} run={async () => {
            setReviewRequired(true);
            try { await props.client.useBundledComputer(status.revision); } finally { await refresh(); }
          }}>Use bundled computer helper</Action>}
          <Action disabled={!props.connected || !status.configuration.helper_executable || query.isFetching || reviewRequired} run={async () => {
            setReviewRequired(true);
            try { await props.client.configureComputer({ revision: status.revision, configuration: { ...status.configuration, enabled: !status.configuration.enabled } }); }
            finally { await refresh(); }
          }}>{status.configuration.enabled ? 'Disable computer helper' : 'Enable computer helper'}</Action>
          <Button variant="ghost" disabled={!props.connected || query.isFetching} onClick={() => void refresh()}>Refresh helper status</Button>
          {reviewRequired && <p role="status">Read current helper configuration before another change. The previous request may have arrived.</p>}
          {(['allow', 'deny'] as const).map((kind) => (
            <div key={kind} {...stylex.props(layout.column, layout.notice)}>
              <strong>{kind === 'allow' ? 'Allowed applications' : 'Denied applications'}</strong>
              <span>{status.configuration[kind]?.join(', ') || 'None'}</span>
            </div>
          ))}
          <Field
            label="Execution-host application"
            description="Use the exact application name or identifier recognized by the host."
          >
            <Input value={app} onChange={(event) => setApp(event.target.value)} />
          </Field>
          <div {...stylex.props(layout.row, layout.wrap)}>
            {(['allow', 'deny'] as const).map((action) => (
              <Action
                key={action}
                disabled={!props.connected || !app.trim()}
                run={async () => {
                  const other = action === 'allow' ? 'deny' : 'allow',
                    name = app.trim();
                  try {
                    await props.client.configureComputer({
                      revision: status.revision,
                      configuration: {
                        ...status.configuration,
                        [action]: [...new Set([...(status.configuration[action] ?? []), name])],
                        [other]: (status.configuration[other] ?? []).filter(
                          (item) => item !== name,
                        ),
                      },
                    });
                    setApp('');
                  } finally {
                    await query.refetch();
                  }
                }}
              >
                {action === 'allow' ? 'Allow app' : 'Deny app'}
              </Action>
            ))}
          </div>
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Action
              disabled={
                !props.connected || !status.configuration.enabled || !status.platform_supported
              }
              run={async () => {
                await props.client.reconnectComputer(status.generation);
                await query.refetch();
              }}
            >
              Reconnect computer helper
            </Action>
            <Action
              disabled={!props.connected || status.state !== 'connected'}
              run={async () => {
                await props.client.disconnectComputer(status.generation);
                await query.refetch();
              }}
            >
              Disconnect computer helper
            </Action>
          </div>
        </>
      )}
    </Section>
  );
}
export function Tools(props: InspectorProps) {
  const [search, setSearch] = useState(''),
    [selected, setSelected] = useState('');
  const schemas = useDetailQuery(props, 'tool.schemas', { session_id: props.session.id });
  const builtins = (schemas.data?.items ?? []).filter(
    (tool) =>
      tool.module !== 'tools' &&
      `${tool.module}.${tool.name}`.toLowerCase().includes(search.toLowerCase()),
  );
  const tools = Object.entries(props.selected.configuration.tools ?? {}).filter(([name]) =>
    name.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <>
      <Section
        title="Built-in tools"
        description="Public direct-call schemas for this agent’s enabled modules. Listing does not start resources, grant authority, or execute a tool."
      >
        <QueryFeedback query={schemas} connected={props.connected} />
        <Field label="Find a tool">
          <Input value={search} onChange={(event) => setSearch(event.target.value)} />
        </Field>
        {builtins.slice(0, 64).map((tool) => {
          const name = `${tool.module}.${tool.name}`;
          return (
            <article key={name} {...stylex.props(layout.column, layout.notice)}>
              <strong>{name}</strong>
              <p>{tool.description}</p>
              <Button variant="ghost" onClick={() => setSelected(selected === name ? '' : name)}>
                Inspect {name} schema
              </Button>
              {selected === name && (
                <CodeBlock
                  code={JSON.stringify(tool.input_schema, null, 2)}
                  label={`${name} input schema`}
                  language="json"
                  maxBytes={32 << 10}
                />
              )}
            </article>
          );
        })}
        {builtins.length > 64 && (
          <Empty>
            Showing 64 matches. Narrow the module or tool name to inspect another schema.
          </Empty>
        )}
        {!builtins.length && schemas.data && (
          <Empty>No matching built-in schemas for this agent.</Empty>
        )}
      </Section>
      <Section
        title="Captured custom tools"
        description="These declarations belong to the selected agent’s exact configuration. Callable authority and executor availability are checked separately."
      >
        {tools.slice(0, 64).map(([name, tool]) => (
          <article key={name} {...stylex.props(layout.column, layout.notice)}>
            <strong>{name}</strong>
            <p>{tool.description}</p>
            <Button variant="ghost" onClick={() => setSelected(selected === name ? '' : name)}>
              Inspect schema
            </Button>
            {selected === name && (
              <>
                <CodeBlock
                  code={JSON.stringify(tool.input_schema, null, 2)}
                  label="Tool input schema"
                  language="json"
                  maxBytes={32 << 10}
                />
                <CodeBlock
                  code={JSON.stringify(tool.output_schema, null, 2)}
                  label="Tool output schema"
                  language="json"
                  maxBytes={32 << 10}
                />
              </>
            )}
          </article>
        ))}
        {tools.length > 64 && (
          <Empty>Showing 64 matches. Narrow the name to inspect another tool.</Empty>
        )}
        {!tools.length && <Empty>No matching custom tools.</Empty>}
      </Section>
      <HostDiagnostics {...props} />
    </>
  );
}

function HostDiagnostics(props: InspectorProps) {
  const query = useQuery({
    queryKey: ['inspector-host-status', props.client.runtimeID, props.client.processEpoch],
    queryFn: async ({ signal }) => {
      const value = await props.client.hosts.status({ signal });
      if (value.process_epoch !== props.client.processEpoch)
        throw new Error('The host process changed. Reconnect to inspect it.');
      return value;
    },
    enabled: props.connected,
    gcTime: 0,
    retry: false,
  });
  return (
    <Section
      title="Execution host"
      description="Observed identity and startup information for this exact connected process."
    >
      <QueryFeedback query={query} connected={props.connected} />
      <code>Runtime {props.client.runtimeID}</code>
      <span>Process epoch {props.client.processEpoch}</span>
      {query.data && (
        <>
          <span>Process {query.data.pid}</span>
          <span>Build {query.data.build || 'Unspecified'}</span>
          <span>Started {query.data.started_at}</span>
          <span>Web gateway: {query.data.web_endpoint || 'Disabled'}</span>
        </>
      )}
    </Section>
  );
}
