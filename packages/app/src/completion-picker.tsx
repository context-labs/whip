import { ErrorNotice } from './error-feedback';
import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { Session } from '@whip/sdk';
import { Button, Combobox, Dialog, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';

/** Suggestions are resolved on the execution host; the browser never scans files. */
export function CompletionPicker({
  session,
  agentId,
  onSelect,
  onClose,
}: {
  session: Session;
  agentId: string;
  onSelect(text: string): void;
  onClose(): void;
}) {
  const connected = useWhipConnection(session.client).state === 'connected';
  const [kind, setKind] = useState('mention');
  const [input, setInput] = useState('');
  const [prefix, setPrefix] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setPrefix(input.replace(/^[@$]/, '')), 200);
    return () => clearTimeout(timer);
  }, [input]);
  const result = useQuery({
    queryKey: [
      'completion',
      session.client.getSnapshot().info?.runtime_id,
      session.rootId,
      agentId,
      kind,
      prefix,
    ],
    queryFn: ({ signal }) =>
      session.client.call(
        'workspace.complete',
        { root_id: session.rootId, agent_id: agentId, kind, prefix, limit: 32 },
        { signal },
      ),
    gcTime: 0,
    enabled: connected,
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title="Insert host context"
      description="Insert a workspace file reference or an available skill into your draft."
    >
      <div {...stylex.props(layout.column)}>
        <Select
          label="Context type"
          value={kind}
          onValueChange={setKind}
          options={[
            { value: 'mention', label: 'Workspace file' },
            { value: 'skill', label: 'Skill' },
          ]}
        />
        <Combobox
          key={kind}
          label="Search on the host"
          value={null}
          loading={result.isFetching}
          onInputValueChange={setInput}
          options={(result.data?.candidates ?? []).map((item) => ({
            value: item.text,
            label: item.text,
            description: item.description,
          }))}
          onValueChange={(text) => {
            onSelect(text);
            onClose();
          }}
        />
        {result.error && connected && <ErrorNotice type="resource" owner={`${session.rootId}:${agentId}:completion`} title="Could not load suggestions" error={result.error} action={<Button variant="ghost" onClick={() => void result.refetch()}>Retry</Button>} />}
        {!connected && <p role="status">Suggestions are unavailable while this host is offline.</p>}
        {result.data?.truncated && (
          <p>More matches are available. Narrow your search.</p>
        )}
        {result.data?.warnings?.map((warning) => (
          <p key={warning} role="status">
            {warning}
          </p>
        ))}
      </div>
    </Dialog>
  );
}
