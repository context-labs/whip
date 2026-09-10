import { useNavigate } from '@tanstack/react-router';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from './context';
import { openNewChat } from './session-tab-routing';
import { layout } from './styles';
import { WelcomeRecovery } from './welcome-recovery';
export function EmptyWorkspace({ missing = false }: { missing?: boolean }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  return <div {...stylex.props(layout.empty)}>
    <h1>{missing ? 'This New Chat is not available' : 'Your workspace is empty'}</h1>
    <p>{missing ? 'This draft may belong to another window or no longer be saved here.' : 'Open a saved session or start a New Chat.'}</p>
    <Button onClick={() => openNewChat(runtime, navigate)}>New Chat</Button>
    <WelcomeRecovery />
  </div>;
}
