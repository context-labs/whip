import { ErrorNotice } from './error-feedback';
import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Session } from '@whip/sdk';
import { readSkillSuggestions } from './skill-suggestions';
import { Button, Combobox, Dialog, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';

/** Suggestions are resolved on the execution host; the browser never scans files. */
export function CompletionPicker({
  session,
  connected,
  onSelect,
  onClose,
}: {
  session: Session;
  connected: boolean;
  onSelect(text: string): void;
  onClose(): void;
}) {
  const clientKey = useMemo(() => crypto.randomUUID(), [session.client]);
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
      session.client.runtimeID,
      session.client.processEpoch,
      clientKey,
      session.id,
      kind,
      prefix,
    ],
    queryFn: ({ signal }) => kind === 'skill'
      ? readSkillSuggestions(session.client, { sessionId: session.id }, prefix, 32, signal)
      : session.client.completeWorkspace({ session_id: session.id, kind: 'mention', prefix, limit: 32 }, { signal }),
    gcTime: 0, retry: false,
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
          key={`${clientKey}:${session.id}:${kind}`}
          label="Search on the host"
          value={null}
          loading={result.isFetching}
          disabled={!connected}
          onInputValueChange={setInput}
          options={(connected ? result.data?.candidates ?? [] : []).map((item) => ({
            value: item.text,
            label: item.text,
            description: item.description,
          }))}
          onValueChange={(text) => {
            if (!connected || !result.data?.candidates.some(item => item.text === text)) return;
            onSelect(text);
            onClose();
          }}
        />
        {result.error && connected && <ErrorNotice type="resource" owner={`${session.client.runtimeID}:${session.id}:completion`} title="Could not load suggestions" error={result.error} action={<Button variant="ghost" onClick={() => void result.refetch()}>Retry</Button>} />}
        {!connected && <p role="status">Suggestions are unavailable while this host is offline.</p>}
        {result.data?.truncated && (
          <p>More matches are available. Narrow your search.</p>
        )}
      </div>
    </Dialog>
  );
}
