// JuniorDeveloper, authored in TypeScript. Registered as `junior-developer-ts`
// it composes the same prompt as the built-in Go definition; the test beside
// this file pins that against the runtime's fixture.
import { defineAgent } from '@whip/sdk/agents';

export const juniorDeveloper = defineAgent({
  id: 'junior-developer-ts',
  instructions: {
    persona: 'You are a junior developer working under review.',
    rules: [
      'Operating rules:',
      '- Keep each change small, and explain what you changed and why in plain language.',
      "- Run the project's tests or build after every change; if you cannot run them, say so.",
      '- Never rewrite history, force-push, delete branches, or remove files you did not create.',
      '- Do not add dependencies or change build, CI, or deployment configuration; ask first.',
      '- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.',
    ].join('\n'),
    projectFiles: ['CLAUDE.md', 'AGENTS.md'],
    skillDiscovery: false,
    standingInstructions: true,
  },
  modules: ['context', 'files', 'shell', 'state', 'artifacts', 'permissions', 'user'],
  capabilities: ['read', 'write', 'shell'],
  surface: { autoTitle: true, goalLoop: false },
});

// Register it and start a session:
//
//   import { createWhipClient } from '@whip/sdk';
//   const client = createWhipClient({ endpoint: 'http://127.0.0.1:8080', clientId: 'agents-example' });
//   await client.connect();
//   const { revision } = await client.agents.register(juniorDeveloper);
//   const created = await client.sessions.create({ cwd: '/path/to/repo', definition: 'junior-developer-ts' }).result();
//
// Or from the terminal, against the same daemon:
//
//   whip run --agent junior-developer-ts --permission-mode automatic "add a unit test for the parser"
