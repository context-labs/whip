import type { Meta, StoryObj } from '@storybook/react-vite';
import { Alert, Avatar, Badge, Button, CodeBlock, Panel, Progress, Row, Skeleton, Stack, StatusIndicator } from '../src';

const meta = { title: 'WHIP/Visual states', parameters: { layout: 'padded' } } satisfies Meta;
export default meta;

/** Synthetic display states composed from the shipped library, with no daemon. */
export const Runtime: StoryObj<typeof meta> = {
  render: () => <Stack>
    <h1>Session activity</h1>
    <Alert title="Permission requested" tone="warning" action={<Row><Button>Allow once</Button><Button variant="ghost">Deny</Button></Row>}>
      The agent wants to write a file in this workspace.
    </Alert>
    <Alert title="Permission approved" tone="success">This request was resolved by another connected client.</Alert>
    <Alert title="Turn interrupted" tone="error" action={<Button>Inspect outcome</Button>}>
      The host restarted. The previous operation was not repeated.
    </Alert>
    <Alert title="Reconnecting" tone="info">Showing retained state while the host is unavailable.</Alert>
    <Panel><Stack>
      <h2>Agent activity</h2>
      <Row><Avatar name="Explore core"/><strong>Explore core</strong><StatusIndicator tone="info">Running</StatusIndicator><Badge>Root agent</Badge></Row>
      <Progress label="Context use" value={42}/>
      <Row><Avatar name="Inspect storage"/><strong>Inspect storage</strong><StatusIndicator tone="warning">Waiting for input</StatusIndicator><Badge>Child agent</Badge></Row>
      <Row><Avatar name="Check protocol"/><strong>Check protocol</strong><StatusIndicator tone="success">Completed</StatusIndicator><Badge>Child agent</Badge></Row>
      <CodeBlock label="Starlark activity" language="starlark" code={'result = agents.get(name="inspect-storage")\nprint(result.status)\n'}/>
    </Stack></Panel>
    <Panel><Stack><strong>Loading current state</strong><Skeleton/><Skeleton/></Stack></Panel>
  </Stack>,
};
