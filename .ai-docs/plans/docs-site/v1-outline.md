# V1 docs outline

Status: implemented and subsequently revised: 21 pages in five sidebar groups.
Introduction was removed; Quickstart is the sole introductory entry, followed by
Download. Quickstart, Download and TypeScript SDK now have body content. The
original proposal below is historical; the current manifest defines navigation.
Scope: titles and headings, not drafted article copy. Entry/legacy redirects and
content-independent library test fixtures are in place. The prior theme-import
fix remains uncommitted alongside this work; no commits made in this task.

## Recommendation

Use **five flat sidebar groups, 22 short pages**. Groups are labels, not landing
pages. Avoid nested submenus and separate overview pages that only repeat links.
Keep the first three Getting Started pages exactly **Introduction**,
**Quickstart**, **Download**, in that order.

A page gets its own entry when someone will deliberately look for it. Small,
interface-specific features remain sections: Terminal tabs inside Desktop,
TUI configuration inside TUI, SSH setup inside Hosts & SSH, uploads inside
Sessions & files. Browser & computer use and Design Mode merit their own pages
because they have distinct setup, permissions and workflows.

## Navigation

```text
Getting Started
  Introduction
  Quickstart
  Download
  Troubleshooting

Using whip
  Desktop
  Browser & computer use
  Design Mode
  TUI
  Headless mode
  Sessions & files

Configuration
  Configuration
  Hosts & SSH
  Providers & models
  Permissions
  Instructions
  Skills
  MCP

Agents & RLM
  RLM
  Agents & subagents
  Messages & inboxes
  Schedules

Developers
  TypeScript SDK
```

This is the approved V1. The user subsequently requested every page and heading
in code; all are now visible as outlines for editing, not final published content. No separate Terminal, TUI Config, SSH, Uploads, Subagents,
Agent communication, Inboxes or provider-by-provider pages yet.

## Page outlines

Each heading below is a proposed H2. Use H3 only for a necessary distinction
(e.g. Desktop/CLI installation), not to turn every paragraph into a section.

### Getting Started

**Introduction**
- What whip does
- Desktop, TUI and headless
- How work runs: sessions, agents and hosts
- What you need

**Quickstart**
- Install whip
- Connect a provider
- Open a project
- Run a task
- Review the result

Keep this one short happy path. Default to Desktop; provide a compact TUI
alternative where the action differs. Do not introduce SSH, MCP, custom agents
or the SDK here. Link Download for install variants rather than reproducing them.

**Download**
- Desktop
- Command line
- Supported platforms
- Updates and release channels

**Troubleshooting**
- Startup and connections
- Provider and model errors
- Approvals and denied actions
- Logs and reporting an issue

This is the symptom index. Detailed fixes live with the feature that owns them;
avoid copying every warning onto this page.

### Using whip

**Desktop**
- Workspace and conversations
- Start, switch and resume sessions
- Inspect activity and changes
- Terminal tabs
- Shortcuts and settings

**Browser & computer use**
- Browser tabs and browser connections
- Give an agent browser access
- Desktop control and system permissions
- Approvals, scope and cleanup

Clearly distinguish browsing a page yourself from granting an agent access,
and native Browser tabs from external Chrome/computer control. Mark availability
and experimental status explicitly; do not imply every host has a local desktop.

**Design Mode**
- Open a preview
- Select elements
- Describe a change
- Choose the conversation and send
- Included evidence and limitations

**TUI**
- Start in a project
- Compose and navigate
- Commands and shortcuts
- Themes and keybindings
- TUI configuration

**Headless mode**
- Run a task
- Input and output formats
- Models and permissions
- Resume a session
- Exit status and cancellation

**Sessions & files**
- Start and resume
- Interrupt, stop and detach
- Fork and rewind
- Reference project files
- Upload files and images

Keep the core lifecycle here so Desktop, TUI and Headless do not explain it three
times. Explain where uploaded data goes and which host resolves a file reference.
A brief workspace description belongs here; no standalone Workspace page yet.

### Configuration

**Configuration**
- Files and locations
- Settings and precedence
- Environment variables
- When changes take effect

A small settings table can sit under Settings and precedence. This page explains
mechanics, not all provider, permission, MCP and TUI options again.

**Hosts & SSH**
- Where work runs
- This Mac and local hosts
- Connect over SSH
- Connect by URL
- Reconnect and manage the host

Use **host** for the machine running work and **daemon** for the process. Avoid
an additional Servers page: MCP servers are a different concept. Cover remote
installation prerequisites, versions and credential/config ownership here.

**Providers & models**
- Connect a provider
- Credentials and account login
- Select a model and effort
- Custom providers
- Usage and cost

One page, compact provider notes/table. No copied marketing model catalogue or
one-page-per-provider scaffold. Explain provider billing versus whip usage
accounting; link agent limits rather than duplicating them.

**Permissions**
- Permission modes
- Approve or deny an action
- Files, commands and integrations
- Subagent inheritance
- Headless and remote work

State the non-sandbox boundary early. Separate having a tool available from
permission to perform its effects. Headless mode links here for the full rules.

**Instructions**
- Project instructions
- Personal instructions
- Discovery and precedence
- What belongs in an instruction

This is the home for AGENTS.md/CLAUDE.md and standing guidance. Keep it distinct
from Skills: instructions are persistent rules; skills are task-specific guidance.
Only list filenames/scopes actually supported by the current release.

**Skills**
- What a skill provides
- Add a skill
- SKILL.md structure
- Discovery and scope
- Use a skill

**MCP**
- Add a server
- Local and remote connections
- Import existing configurations
- Discover and authorize tools
- Refresh, reconnect and remove

Process launch/import trust belongs here; cross-link Permissions for tool-effect
approval. Do not imply an imported server is safe merely because a tool needs
approval. Keep transport-specific examples in this page, not separate pages.

### Agents & RLM

**RLM**
- How recursive work runs
- Context and compaction
- State and memory
- Budgets and limits

Spell out Recursive Language Model once. Explain the observable model: the root
agent can delegate focused work, retrieve context and continue across turns.
Users should not need to know Starlark, V8, engine accounting or database details
to use whip. Distinguish retained history from the model's active context, and
agent state from permanent project instructions.

**Agents & subagents**
- Root agents and delegated work
- Agent definitions
- Inspect and steer a subagent
- Permissions and limits
- Stop work and review results

Built-in/custom agent selection belongs here. Put TypeScript definition/tool
syntax in the SDK page, linked from Agent definitions. Explain that child work
and messages are separate from the parent finishing a turn.

**Messages & inboxes**
- Direct a message
- Queue or steer
- Agent-to-agent communication
- Pending, delivered and completed
- Notifications and results

Describe delivery and visible state, not a tool-by-tool mailbox API listing.
Distinguish the human composer queue from agent mailboxes in this page. Keep
exact low-level API contracts in the engineering manual/SDK reference.

**Schedules**
- Create a schedule
- Timing and recurrence
- What wakes and where it runs
- Inspect and cancel
- Offline hosts and missed work

Verify actual timezone, persistence and missed-run behavior before drafting;
section headings are questions the page must answer, not new product promises.

### Developers

**TypeScript SDK**
- Availability and installation
- Connect to a host
- Create a session and submit work
- Events, results and cancellation
- Define agents and custom tools
- State views and React

Keep this a focused entry/reference page for V1, not an SDK microsite. Link the
package README for exhaustive contracts. Current source calls the SDK a private
workspace package: document the real installation route rather than inventing a
public npm install command. Split client usage from agent authoring later only
if the real examples make this page unwieldy.

## What matters most for V1

The important conceptual distinctions should be repeated briefly where needed,
then linked to one owner page:

1. **Interface versus host.** Desktop/TUI/SDK connect; the host owns execution,
   credentials, configuration and session data. Closing a view is not stopping work.
2. **Session versus agent.** A session contains ongoing work; a root may delegate
   to children. Children, messages and schedules can outlive one parent turn.
3. **Context versus history.** Not everything retained by whip is in the current
   model prompt. Retrieval, compaction and state are distinct from instructions.
4. **Capability versus permission.** Available tools are not blanket consent;
   Ask is not an OS sandbox. Remote work and headless approval need explicit limits.
5. **Instructions versus skills versus MCP.** Persistent guidance, task guidance,
   and external tool connections should not be bundled into one vague Extensions page.
6. **Local versus uploaded files.** Explain the working folder, host paths and
   uploaded content in user terms, without requiring the storage implementation.

Sessions, context, instructions and troubleshooting are the useful additions to
the user's initial topic list. Everything else requested has a home above.

## Writing contract

- Reference-first: one sentence stating purpose, then useful headings.
- Aim for 3–5 sections; use six only where it prevents a second page.
- Short paragraphs, direct verbs, sentence-case headings. No "In this guide,"
  feature pitches, repeated conclusions or unnecessary introductions.
- Explain behavior/defaults/limits, then one minimal example. Tables for options.
- Use UI labels and command names exactly. Define daemon/RLM/MCP once, then use
  plain language. Avoid wire-format, scheduler and storage terminology in user pages.
- Put cautions beside the relevant action. Say what does not happen automatically.
- Shared settings have one owner page. Interface pages show the action and link
  to it; they do not maintain a second configuration reference.
- Quickstart is the one walkthrough. Other pages should work when entered from
  search, with no expectation that the reader has read preceding pages.

## Scope guardrails

Do not add for V1: per-model/per-provider pages, every slash command as a page,
every built-in tool as a page, engine internals, wire protocol/database reference,
benchmark methodology, release engineering, evaluator infrastructure, a mobile
section before it is supported, or empty integrations/how-to/FAQ collections.

Terminal tabs are a human shell, not the TUI and not agent shell execution.
Design Mode selects UI evidence, not an independent execution engine.
Browser/computer support needs a small actual-availability note, not several
empty platform-specific pages.

## Implementation plan after outline approval

1. Agree this nav and headings. No page prose or visual redesign is needed first.
2. Split the current Getting started content into Introduction and Quickstart;
   turn Installation into Download. Retain useful existing content rather than
   rewriting every page from scratch.
3. Rehome existing Desktop/CLI/configuration/permissions pages. Keep current URLs
   where their meaning survives; add redirects for actual renames. Do not force
   URL prefixes to mirror sidebar group labels.
4. Default entry: `/` and `/docs` go to Introduction. Keep
   `/docs/getting-started` as an alias to Introduction and the old installation
   URL as an alias to Download. Redirects must work in both static output and dev.
   Header Download should lead to the new Download page; release links live there.
5. Implement five ordered nav groups in the existing frontmatter/manifest system.
   Existing schema has three fixed group ids; update it once, with ordering and
   route/link tests. No new content framework or navigation tree abstraction.
6. Draft the essentials first: first three pages, interfaces, Sessions & files,
   Hosts & SSH, Providers & models, Permissions. Then instructions/integrations,
   RLM collaboration and SDK. Publish only complete pages, no "coming soon" entries.
7. Verify UI/CLI names, actual supported platforms and feature status against the
   intended release. Browser tabs are currently described as experimental; SDK
   packaging is currently private. Do not turn an outline into a support promise.
8. Validate internal links/anchors, redirect aliases and no-JS reading. Keep old
   planning documents historical; the approved outline becomes the editorial plan.

URL/entry behavior above is a recommendation for the navigation change, not a
change performed by this planning task. No additional decision is needed to
review the outline; download/SDK availability is a factual publishing check.

## Research notes

Read the requested reference sites' main content and navigation. Extraction
services may cache content; observations below concern organization, not their
current product claims, prices, model lists or release versions.

- [Command Code docs](https://commandcode.ai/docs): clear Introduction/Quickstart
  entry, direct topic pages such as Permissions, Skills, MCP, Sessions,
  Context and Headless, with separate CLI/slash-command references. Borrow the
  recognizable topic names and dedicated Quickstart. Do not borrow the promotional
  home-page copy, pricing-heavy sidebar or its much wider V1 page inventory.
- [Command Code Quickstart](https://commandcode.ai/docs/quickstart): install,
  authenticate, open a project, first prompt. Good single-workflow progression;
  whip does not need its product-specific marketing and repeated install details.
- [OpenCode v2 docs](https://opencode.ai/v2/docs/): concise Intro/Config/Troubleshooting
  entry and direct Configure pages. Its top-level Docs/CLI/Build/API split
  separates audiences. Borrow the audience boundaries, not a multi-site nav system.
- [OpenCode CLI](https://opencode.ai/v2/docs/cli/): keeps interactive entry and
  automation distinct; explains the background service where users encounter it.
- [OpenCode Build](https://opencode.ai/v2/docs/build/): developer extension/client
  work is distinct from day-to-day use. Keep whip SDK under Developers.

Whip source anchors used for the conceptual boundaries:
- `docs/desktop.md` — Connections and lifetime; Browser tabs; Design Mode;
  Terminal tabs. Confirms host ownership and the human-shell distinction.
- `docs/features.md` — Recursive agent runtime; Messages and collaboration;
  Focused context; MCP; Provider connections; Skills; model budgets; TUI commands.
- `docs/rlm-runtime.md` — capabilities, messages/wakeups, agent state, schedules,
  permissions, accounting and recovery. Use for factual checks, not public IA.
- `docs/setup.md`, `docs/tools.md`, `docs/models-providers.md` — setup, integration
  trust, credentials and available configuration.
- `packages/sdk/README.md` — attach/submit, operation lifetimes, content/approvals,
  agent authoring and package availability.

Original research was planning-only. The subsequent user request implemented the
outline in code; tests/builds pass. No dependency installs, commits or pushes.
