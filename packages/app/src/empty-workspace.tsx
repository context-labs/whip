import { useNavigate } from '@tanstack/react-router';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from './context';
import { openNewChat } from './session-tab-routing';
import { layout } from './styles';
import { WelcomeRecovery } from './welcome-recovery';
export function EmptyWorkspace({ missing = false, subject = 'draft' }: { missing?: boolean; subject?: 'draft' | 'terminal' }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  return <div {...stylex.props(layout.empty)}>
    <h1>{!missing ? 'Your workspace is empty' : subject === 'terminal' ? 'This terminal is not open here' : 'This New Chat is not available'}</h1>
    <p>{!missing ? 'Open a saved session or start a New Chat.' : subject === 'terminal' ? 'Its tab may belong to another window, or its shell has ended. Open a new terminal from a pane menu.' : 'This draft may belong to another window or no longer be saved here.'}</p>
    <Button onClick={() => openNewChat(runtime, navigate)}>New Chat</Button>
    <WelcomeRecovery />
  </div>;
}
