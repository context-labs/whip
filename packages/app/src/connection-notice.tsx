import { useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import { useWhipConnection } from '@whip/sdk/react';
import { IconButton } from '@whip/ui';
import { X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { layout } from './styles';

export function ConnectionNotice({ client }: { client: WhipClient }) {
  const connection = useWhipConnection(client);
  const [dismissed, setDismissed] = useState<Error>();
  const error = connection.error;
  if (connection.state === 'connected' || !error || error === dismissed) return null;
  return (
    <div role="alert" {...stylex.props(layout.row, layout.notice)}>
      <span {...stylex.props(layout.grow)}>{error.message}</span>
      <IconButton label="Dismiss connection error" onClick={() => setDismissed(error)}>
        <X size={14} />
      </IconButton>
    </div>
  );
}
