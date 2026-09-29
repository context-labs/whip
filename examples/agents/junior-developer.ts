// A data-only native definition. Registration grants no file or shell authority.
import { defineAgent } from '@whip/sdk/agents';

export const juniorDeveloper = defineAgent({
  id: 'junior-developer-ts', name: 'Junior Developer',
  defaults: {
    instructions: {
      project_root: null,
      text: [
        'You are a junior developer working under review.', '', 'Operating rules:',
        '- Keep each change small, and explain what you changed and why in plain language.',
        "- Run the project's tests or build after every change; if you cannot run them, say so.",
        '- Never rewrite history, force-push, delete branches, or remove files you did not create.',
        '- Do not add dependencies or change build, CI, or deployment configuration; ask first.',
        '- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.',
      ].join('\n'),
      project_files: ['CLAUDE.md', 'AGENTS.md'], discover_skills: false,
      standing_instructions: true, skill_roots: null,
    },
    modules: ['context', 'files', 'shell', 'state', 'artifacts', 'permissions', 'user'],
    mcp_servers: { all: false, servers: [] }, automatic_title: true, goals_enabled: false,
    children: {},
  }, tools: [], hooks: {}, output: null,
});

// const client = await Client.connect(unixSocket(socket), { clientID: 'junior-example' });
// const definition = await client.agents.register(juniorDeveloper);
// const created = await client.createTree({ definition: definition.ref,
//   working_directory: '/path/to/repo', metadata: { title: null, pinned: false, archived: false }, overrides: {} }, crypto.randomUUID());
// Keep the creation ID/payload for recovery. Inspect and approve requested operations
// explicitly; selecting a definition never grants its declared modules authority.
