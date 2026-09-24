import { ErrorNotice } from '../error-feedback';
import { useState } from 'react';
import { Badge, Button, CodeBlock, Field, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { mcpRefreshNotice } from '../mcp-refresh';
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
type MCPImportSource = 'claude' | 'codex' | 'project' | 'opencode';
const importSources: ReadonlyArray<{ source: MCPImportSource; label: string }> = [
  { source: 'claude', label: 'Claude user file (~/.claude.json)' },
  { source: 'codex', label: 'Codex config (~/.codex/config.toml)' },
  { source: 'project', label: 'Project .mcp.json (repository-authored, off by default)' },
  { source: 'opencode', label: 'OpenCode config (~/.config/opencode/opencode.json)' },
];
// Controls follow what the daemon can honor for a row in its current state.
// Blocked and unreadable rows are not live servers and get none.
function mcpActions(status: string): readonly MCPAction[] {
  switch (status) {
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
// Every control names its subject and its real scope: enable/disable act on
// this session only (the host keeps its configuration), reconnect is a
// request the daemon may still be carrying out when the command succeeds.
const mcpActionLabel: Record<MCPAction, (name: string) => string> = {
  reconnect: (name) => `Reconnect ${name}`,
  enable: (name) => `Enable ${name} for this session`,
  disable: (name) => `Disable ${name} for this session`,
};
const mcpActionOutcome: Record<MCPAction, string> = {
  reconnect: 'Reconnect requested',
  enable: 'Enabled for this session',
  disable: 'Disabled for this session',
};
export function MCP(props: InspectorProps) {
  const runtime = useRuntime();
  const query = useDetailQuery(props, 'mcp.status', {}, true);
  const imports = useDetailQuery(props, 'mcp.import.status', {});
  const [refreshNotice, setRefreshNotice] = useState('');
  return (
    <>
      <Section
        title="MCP servers"
        description="Server processes, credentials, and delegated authority belong to the execution host. Enable and disable apply to this session only; add or remove servers on the host with whip mcp add and whip mcp remove."
      >
        {props.view.session.client.supports('runtime', 'mcp.refresh') ? (
          <Action disabled={!props.connected} run={async () => {
            setRefreshNotice('');
            const outcome = await runtime.run(props.view.session.mcp.refresh(), 'MCP configuration refreshed');
            setRefreshNotice(mcpRefreshNotice(outcome.result));
          }}>
            Refresh MCP configuration for this session
          </Action>
        ) : <p {...stylex.props(layout.muted)}>This host does not support live MCP refresh. Update its daemon or start a new session after changing server configuration.</p>}
        {refreshNotice && <p role="status" {...stylex.props(layout.muted)}>{refreshNotice}</p>}
        <QueryFeedback query={query} view={props.view} />
        {query.data?.result?.length === 0 && (
          <Empty>No MCP servers are configured for this session.</Empty>
        )}
        {query.data?.result?.map((server) => {
          const actions = mcpActions(server.status);
          return (
            <article key={server.name} {...stylex.props(layout.column, layout.notice)}>
              <div {...stylex.props(layout.row)}>
                <strong>{server.name}</strong>
                <Badge>{server.status}</Badge>
              </div>
              <span {...stylex.props(layout.muted)}>
                {server.status === 'unreadable'
                  ? server.source || 'discovery source'
                  : `${server.tools ?? 0} tools · ${server.source || 'host configuration'}`}
              </span>
              {server.note && <p>{server.note}</p>}
              {server.error && <ErrorNotice type="resource" owner={`mcp:${server.name}`} title={`${server.name} needs attention`} error={server.error} />}
              {actions.length > 0 && (
                <div {...stylex.props(layout.row, layout.wrap)}>
                  {actions.map((action) => (
                    <Action
                      key={action}
                      disabled={!props.connected}
                      run={() =>
                        runtime.run(
                          props.view.session.command(`mcp.${action}`, { name: server.name }),
                          mcpActionOutcome[action],
                        )
                      }
                    >
                      {mcpActionLabel[action](server.name)}
                    </Action>
                  ))}
                </div>
              )}
            </article>
          );
        })}
      </Section>
      <Section
        title="Host import defaults"
        description="Which files on the execution host feed MCP definitions into sessions. Saved to host configuration; this session reloads when idle. Enabling a source runs those servers' programs at session start."
      >
        <QueryFeedback query={imports} view={props.view} />
        {imports.data?.result &&
          importSources.map(({ source, label }) => (
            <div key={source} {...stylex.props(layout.settingsRow)}>
              <span>
                {label} · {imports.data!.result![source] ? 'Enabled' : 'Disabled'}
              </span>
              <Action
                disabled={!props.connected}
                run={() =>
                  runtime.run(
                    props.view.session.command('mcp.import.configure', {
                      source,
                      enabled: !imports.data!.result![source],
                    }),
                    'Saved to host configuration',
                  )
                }
              >
                {imports.data!.result![source] ? 'Disable' : 'Enable'} {source} imports
              </Action>
            </div>
          ))}
      </Section>
    </>
  );
}
function LSP(props: InspectorProps) {
  const query = useDetailQuery(props, 'lsp.status', {}, true);
  return (
    <Section
      title="Language servers"
      description="Health and workspace roots on the execution machine. Per-file diagnostics and editing are outside this release."
    >
      <QueryFeedback query={query} view={props.view} />
      {query.data?.result?.length === 0 && <Empty>No language servers are active.</Empty>}
      {query.data?.result?.map((server, index) => (
        <article
          key={`${server.name}:${server.root}:${index}`}
          {...stylex.props(layout.column, layout.notice)}
        >
          <div {...stylex.props(layout.row)}>
            <strong>{server.name}</strong>
            <Badge>{server.state}</Badge>
          </div>
          <code>{server.root}</code>
          {server.error && <ErrorNotice type="resource" owner={`lsp:${server.name}:${server.root}`} title={`${server.name} needs attention`} error={server.error} />}
        </article>
      ))}
    </Section>
  );
}
function Browser(props: InspectorProps) {
  const runtime = useRuntime();
  const query = useDetailQuery(props, 'browser.status', {});
  const [driver, setDriver] = useState('rod');
  return (
    <Section
      title="Browser automation"
      description="This controls browsers on the execution host, not the browser displaying this application."
    >
      <QueryFeedback query={query} view={props.view} />
      {query.data?.result && (
        <>
          <p>
            {query.data.result.enabled
              ? `Available · current driver: ${query.data.result.driver}`
              : 'Browser automation is unavailable on this execution host.'}
          </p>
          <Field label="Browser driver">
            <Select
              label="Browser driver"
              value={driver}
              onValueChange={setDriver}
              options={[
                { value: 'rod', label: 'Rod' },
                { value: 'chromedp', label: 'ChromeDP' },
              ]}
            />
          </Field>
          <Action
            disabled={!props.connected || !query.data.result.enabled}
            run={() =>
              runtime.run(
                props.view.session.command('browser.set_driver', { driver }),
                'Change browser driver',
              )
            }
          >
            Apply driver
          </Action>
        </>
      )}
    </Section>
  );
}
function Computer(props: InspectorProps) {
  const runtime = useRuntime();
  const query = useDetailQuery(props, 'computer.status', {});
  const [app, setApp] = useState('');
  const policy = query.data?.result;
  return (
    <Section
      title="Computer app policy"
      description="Application access is evaluated on the execution host. These rules do not change browser-client permissions."
    >
      <QueryFeedback query={query} view={props.view} />
      {policy && (
        <>
          <p>
            {policy.enabled
              ? `Available · default ${policy.default_deny ? 'deny' : 'allow'}`
              : 'Computer automation is unavailable on this execution host.'}
          </p>
          {(['allowed', 'denied', 'session_allowed', 'session_denied'] as const).map((kind) => (
            <div key={kind} {...stylex.props(layout.column, layout.notice)}>
              <strong>{kind.replaceAll('_', ' ')}</strong>
              <span>{policy[kind]?.join(', ') || 'None'}</span>
            </div>
          ))}
          <Field
            label="Execution-host application"
            description="Use the app name or identifier recognized by the host."
          >
            <Input value={app} onChange={(event) => setApp(event.target.value)} />
          </Field>
          <div {...stylex.props(layout.row, layout.wrap)}>
            {(['allow', 'deny'] as const).map((action) => (
              <Action
                key={action}
                disabled={!props.connected || !policy.enabled || !app.trim()}
                run={() =>
                  runtime.run(
                    props.view.session.command(`computer.${action}`, { app }),
                    `${action} computer app`,
                  )
                }
              >
                {action === 'allow' ? 'Allow app' : 'Deny app'}
              </Action>
            ))}
          </div>
        </>
      )}
    </Section>
  );
}
function Tools(props: InspectorProps) {
  const query = useDetailQuery(props, 'tool.schema', {});
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState('');
  const info = props.view.session.client.getSnapshot().info;
  const definitions = (query.data?.result ?? []).filter((tool) =>
    tool.function.name.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <>
      <Section
        title="Built-in tools"
        description="Public schemas of whip's own tools. MCP tools are counted per server under MCP servers; availability remains subject to permissions and delegated authority."
      >
        <QueryFeedback query={query} view={props.view} />
        <Field label="Find a tool">
          <Input value={search} onChange={(event) => setSearch(event.target.value)} />
        </Field>
        {definitions.slice(0, 64).map((tool) => (
          <article key={tool.function.name} {...stylex.props(layout.column, layout.notice)}>
            <strong>{tool.function.name}</strong>
            <p>{tool.function.description}</p>
            <Button
              variant="ghost"
              onClick={() =>
                setSelected((value) => (value === tool.function.name ? '' : tool.function.name))
              }
            >
              Inspect schema
            </Button>
            {selected === tool.function.name && (
              <CodeBlock
                language="json"
                code={JSON.stringify(tool.function.parameters, null, 2)}
                label="Tool input schema"
                maxBytes={32 << 10}
              />
            )}
          </article>
        ))}
        {definitions.length > 64 && (
          <Empty>Showing 64 matches. Narrow the tool name to inspect another schema.</Empty>
        )}
        {!definitions.length && !query.isLoading && <Empty>No matching tool schemas.</Empty>}
      </Section>
      <Section title="Execution host">
        {info && (
          <>
            <p>
              {info.host_platform} / {info.host_architecture}
            </p>
            <code>Runtime {info.runtime_id}</code>
            <span>
              Generation {info.generation} · build {info.build_id}
            </span>
            <span {...stylex.props(layout.muted)}>
              Protocol {info.protocol_major}.{info.protocol_minor} ·{' '}
              {info.limits.root_subscriptions} root subscriptions per connection
            </span>
            <CodeBlock
              code={JSON.stringify(info.limits, null, 2)}
              label="Host limits"
              language="json"
              maxBytes={8192}
            />
          </>
        )}
      </Section>
    </>
  );
}
