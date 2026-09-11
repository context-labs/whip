import { useContext, useState, type ReactNode } from 'react';
import { Alert, CopyButton, IconButton } from '@whip/ui';
import { X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { typography } from '@whip/ui/tokens.stylex';
import { RuntimeContext } from './context';
import { errorMessage } from './platform';

/** Ownership determines placement; a technical cause never determines scope. */
export type ErrorType = 'application' | 'host' | 'session' | 'turn' | 'execution'
  | 'submission' | 'resource' | 'action' | 'validation';

export interface ErrorNoticeProps {
  type: ErrorType;
  owner: string;
  error: unknown;
  title?: string;
  action?: ReactNode;
  onDismiss?: () => void;
  tone?: 'error' | 'warning' | 'neutral';
}

const titles: Record<ErrorType, string> = {
  application: 'Whip needs attention', host: 'Connection unavailable',
  session: 'This session could not load', turn: 'This turn failed',
  execution: 'This execution failed', submission: 'Your message could not be sent',
  resource: 'This content could not load', action: 'This action could not complete',
  validation: 'Check this value',
};

/** No error store: the existing state owner renders its own feedback here. */
export function ErrorNotice({ type, owner, error, title, action, onDismiss, tone = 'error' }: ErrorNoticeProps) {
  const runtime = useContext(RuntimeContext);
  const [copyError, setCopyError] = useState<string>();
  if (error == null || error === '' || (typeof error === 'object' && 'name' in error && error.name === 'AbortError')) return null;
  const message = errorMessage(error);
  const identity = `${type}:${owner}:${message}`;
  return <div data-error-type={type} data-error-owner={owner} {...stylex.props(styles.container)}>
    <Alert tone={tone} title={title ?? titles[type]} action={onDismiss && <IconButton label={`Dismiss ${type} error`} onClick={onDismiss}><X size={14} /></IconButton>}>
      {type === 'validation' ? <p {...stylex.props(styles.message)}>{message}</p> : <details key={message} {...stylex.props(styles.details)}>
        <summary>Error details</summary>
        <pre {...stylex.props(styles.message)}>{message}</pre>
        {runtime?.platform?.copy && <CopyButton text={message} label="Copy error" copy={async text => { await runtime.platform.copy(text); setCopyError(undefined); }}
          onError={() => setCopyError(identity)} />}
        {copyError === identity && <p role="status">Could not copy. Select the details to copy them manually.</p>}
      </details>}
      {action && <div {...stylex.props(styles.actions)}>{action}</div>}
    </Alert>
  </div>;
}

const styles = stylex.create({
  container: { minWidth: 0, maxWidth: '100%', overflowWrap: 'anywhere' },
  details: { marginTop: 6, fontSize: typography.size12 },
  message: { margin: '6px 0', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '30dvh', overflowY: 'auto', fontSize: typography.size12 },
  actions: { display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 8 },
});
