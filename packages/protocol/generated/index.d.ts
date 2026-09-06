// Generated from Go wire types. Run npm run generate.
export interface Accepted {
  accepted: boolean;
}

export interface AgentCancelParams {
  id: string;
  turn_id: string;
}

export interface AgentInputParams {
  id: string;
  text: string;
  delivery?: string;
}

export type AgentListResult =
  | null
  | {
      id: string;
      root_id: string;
      parent_id: string;
      name: string;
      model: string;
      provider: string;
      effort: string;
      cwd: string;
      report: string;
      status: string;
      pending_mail: number;
      lifecycle_phase: string;
      blocking_reason: string;
      terminal_cause: string;
      allowed_controls: null | string[];
    }[];

export interface AgentSubmitResult {
  agent_id: string;
  inbox_seq: string;
  kind?: string;
  status: string;
}

export interface AgentTranscriptResult {
  cursor: string;
  agent: {
    id: string;
    root_id: string;
    parent_id: string;
    name: string;
    model: string;
    provider: string;
    effort: string;
    cwd: string;
    report: string;
    status: string;
    pending_mail: number;
    lifecycle_phase: string;
    blocking_reason: string;
    terminal_cause: string;
    allowed_controls: null | string[];
  };
  page: {
    history_revision: string;
    through_seq: number;
    next_seq: number;
    has_more: boolean;
    messages:
      | null
      | {
          role?: string;
          authored?: boolean;
          sent_at?: null | string;
          seq: number;
          message?: null | {
            role: string;
            content: string;
            tool_calls?:
              | null
              | {
                  id: string;
                  type: string;
                  function: {
                    name: string;
                    arguments: string;
                  };
                  duration_ms?: number;
                  exit_code?: number;
                }[];
            tool_call_id?: string;
            name?: string;
            authored?: boolean;
            sent_at?: null | string;
            usage?: null | {
              prompt_tokens: number;
              completion_tokens: number;
              prompt_tokens_details?: null | {
                cached_tokens: number;
              };
            };
            model?: string;
            rewound_from?: string;
          };
          body?: null | {
            inline?: unknown;
            text?: null | string;
            binary?: string | null;
            reference_id: string;
            digest: string;
            size: string;
            media_type: string;
            source: string;
          };
        }[];
  };
  presentation?:
    | null
    | {
        seq: string;
        kind: string;
        payload: unknown;
      }[];
  inbox?:
    | null
    | {
        root_id: string;
        agent_id: string;
        seq: string;
        kind: string;
        status: string;
        payload: {
          inline?: unknown;
          text?: null | string;
          binary?: string | null;
          reference_id: string;
          digest: string;
          size: string;
          media_type: string;
          source: string;
        };
      }[];
}

export interface BoundedTranscriptPage {
  history_revision: string;
  through_seq: number;
  next_seq: number;
  has_more: boolean;
  messages:
    | null
    | {
        role?: string;
        authored?: boolean;
        sent_at?: null | string;
        seq: number;
        message?: null | {
          role: string;
          content: string;
          tool_calls?:
            | null
            | {
                id: string;
                type: string;
                function: {
                  name: string;
                  arguments: string;
                };
                duration_ms?: number;
                exit_code?: number;
              }[];
          tool_call_id?: string;
          name?: string;
          authored?: boolean;
          sent_at?: null | string;
          usage?: null | {
            prompt_tokens: number;
            completion_tokens: number;
            prompt_tokens_details?: null | {
              cached_tokens: number;
            };
          };
          model?: string;
          rewound_from?: string;
        };
        body?: null | {
          inline?: unknown;
          text?: null | string;
          binary?: string | null;
          reference_id: string;
          digest: string;
          size: string;
          media_type: string;
          source: string;
        };
      }[];
}

export interface BrowserDriverParams {
  driver: string;
}

export interface BrowserStatusResult {
  enabled: boolean;
  driver?: string;
}

export interface BudgetCapParams {
  id: string;
  kind: string;
  limit: string;
}

export interface BudgetState {
  kind: string;
  limit: string;
  used: string;
  reserved: string;
  remaining: string;
}

export interface CancelParams {
  turn_id?: string;
  target_command_id?: string;
}

export interface CapabilityRecord {
  id: string;
  root_id: string;
  agent_id: string;
  issuer_agent_id: string;
  operations: null | string[];
  scopes: null | string[];
  mcp:
    | null
    | {
        server: string;
        tool: string;
        definition: string;
      }[];
  mcp_all: boolean;
  generation: string;
  status: string;
  expires_at: string;
  created_at: string;
  updated_at: string;
}

export interface CatalogRevision {
  revision: string;
}

export interface CheckpointParams {
  reason?: string;
}

export interface CommandParams {
  command_id: string;
  scope: string;
  root_id?: string;
  operation: string;
  payload?: unknown;
}

export interface CommandResult {
  content?: null | {
    reference_id: string;
    digest: string;
    size: string;
    media_type?: string;
    source?: string;
  };
  operation: string;
  result?: unknown;
  failure?: null | {
    data?: null | {
      kind: string;
    };
    code: number;
    message: string;
  };
  command_id: string;
  ingress_seq: string;
  status: string;
}

export interface CommandStatusParams {
  command_id: string;
}

export type CompactionListResult =
  | null
  | {
      seq: number;
      cutoff: number;
      summary: string;
    }[];

export interface CompactionParams {
  model?: string;
  provider?: string;
}

export interface CompactionResult {
  cutoff: number;
  model?: string;
  usage: {
    prompt_tokens: number;
    completion_tokens: number;
    prompt_tokens_details?: null | {
      cached_tokens: number;
    };
  };
}

export interface CompactionRetryResult {
  undone: boolean;
  sequence?: number;
}

export interface CompactionSettingsResult {
  model?: string;
  provider?: string;
  builtin_default: boolean;
}

export interface CompletionParams {
  root_id: string;
  agent_id?: string;
  kind: string;
  prefix: string;
  limit: number;
}

export interface CompletionResult {
  warnings?: null | string[];
  candidates:
    | null
    | {
        text: string;
        description: string;
      }[];
  truncated: boolean;
}

export interface ComputerAppParams {
  app: string;
}

export interface ComputerStatusResult {
  enabled: boolean;
  default_deny: boolean;
  allowed: null | string[];
  denied: null | string[];
  session_allowed: null | string[];
  session_denied: null | string[];
}

export interface ConfigurationUpdate {
  import_claude?: null | boolean;
  import_codex?: null | boolean;
  revision: string;
  default_model?: null | string;
  default_provider?: null | string;
  default_effort?: null | string;
  compact_model?: null | string;
  compact_provider?: null | string;
  compact_percent?: null | number;
  goal_max_rounds?: null | number;
  max_retries?: null | number;
}

export interface ContentEventPayload {
  content: {
    reference_id: string;
    digest: string;
    size: string;
    media_type?: string;
    source?: string;
  };
  truncated: true;
}

export interface ContentHandle {
  reference_id: string;
  digest: string;
  size: string;
  media_type?: string;
  source?: string;
}

export interface ContentReadParams {
  root_id: string;
  agent_id?: string;
  reference_id: string;
  offset: string;
  limit: number;
}

export interface ContentReadResult {
  data: string | null;
  content: {
    reference_id: string;
    digest: string;
    size: string;
    media_type?: string;
    source?: string;
  };
}

export interface ContextAuditResult {
  working_directory: string;
  rows:
    | null
    | {
        label: string;
        bytes?: number;
        note?: string;
      }[];
}

export interface CreateSessionParams {
  kind: string;
  cwd: string;
  model: string;
  provider: string;
}

export interface EffortParams {
  effort: string;
  persist_default: boolean;
}

export interface EffortResult {
  effort: string;
}

export interface Empty {}

export interface EmptyParams {}

export interface EnrollIdentityParams {
  public_key: string | null;
  tty_confirmed?: boolean;
  authorized_by?: string;
  signature?: string | null;
}

export interface EventNotification {
  event: {
    subscription_id?: string;
    root_id: string;
    seq: string;
    kind: string;
    payload?: unknown;
  };
}

export interface ForkParams {
  expected_revision: string | null;
  title?: string;
  cut?: number;
}

export interface GoalContextParams {
  window?: number;
}

export interface GoalResult {
  goal: string;
}

export interface HistoryPageParams {
  root_id: string;
  agent_id: string;
  after_seq?: number;
  before_seq?: number;
  through_seq: number;
  revision?: string | null;
  limit: number;
  max_bytes: number;
  recent?: boolean;
}

export interface IDParams {
  id: string;
}

export interface IdentityResult {
  client_id: string;
  kind: string;
  nonce: string | null;
}

export interface IdentityStatusResult {
  client_id: string;
  kind: string;
  paired: boolean;
  enrollment_open: boolean;
}

export interface InitializeParams {
  protocol_major: number;
  build_id: string;
  client_kind: string;
  client_id: string;
  capabilities?: null | string[];
}

export interface InitializeResult {
  operations:
    | null
    | {
        name: string;
        surface: string;
        execution: string;
        permission: string;
        sensitive?: boolean;
      }[];
  limits: {
    frame_bytes: number;
    connections: number;
    in_flight_requests: number;
    outbound_messages: number;
    outbound_bytes: string;
    root_subscriptions: number;
    content_chunk_bytes: number;
    upload_bytes: string;
  };
  negotiated_capabilities: null | string[];
  protocol_minor: number;
  runtime_id: string;
  connection_id: string;
  host_platform: string;
  host_architecture: string;
  network_endpoint?: string;
  protocol_major: number;
  build_id: string;
  generation: string;
  pid?: number;
  started_at?: string;
  capabilities: null | string[];
  nonce: string | null;
}

export type LSPListResult =
  | null
  | {
      name: string;
      root?: string;
      state: string;
      error?: string;
    }[];

export interface LifecycleEvent {
  turn_id?: string;
  root_id?: string;
  agent_id?: string;
  sender_agent_id?: string;
  inbox_seq?: string;
  inbox_kind?: string;
  delivery?: string;
  message_id?: string;
  phase?: string;
  status?: string;
  terminal_cause?: string;
  command_client_id?: string;
  command_id?: string;
  operation_id?: string;
  trace_id?: string;
  schedule_id?: number;
  slot?: string;
  error?: string;
  acknowledged_inbox?: string[];
  subscription_id?: string;
  key?: string;
  version?: string;
  expected_version?: string;
  restored?: null | string[];
  not_restored?:
    | null
    | {
        name: string;
        reason: string;
      }[];
  attempt?: string;
  budget_kind?: string;
  amount?: string;
  limit?: string;
  used?: string;
  reserved?: string;
  capability_id?: string;
  generation?: string;
  permission_id?: string;
  operation?: string;
  canonical_path?: string;
  request_digest?: string;
  command?: string;
  rule?: string;
  rule_source?: string;
  question_id?: string;
  question?: string;
  options?:
    | null
    | {
        label: string;
        description?: string;
      }[];
  multiple?: boolean;
  answer?: null | string[];
  dismissed?: boolean;
}

export interface ListParams {
  limit?: number;
}

export interface MCPAttachParams {
  servers: {
    [k: string]: {
      command?: null | string[];
      env?: {
        [k: string]: string;
      };
      cwd?: string;
      url?: string;
      headers?: {
        [k: string]: string;
      };
      enabled?: null | boolean;
      note?: string;
      startup_timeout?: number;
      tool_timeout?: number;
      source?: string;
      origin?: string;
    };
  };
}

export interface MCPImportParams {
  source: string;
  enabled: boolean;
}

export interface MCPImportStatusResult {
  claude: boolean;
  codex: boolean;
}

export type MCPListResult =
  | null
  | {
      name: string;
      status: string;
      note?: string;
      error?: string;
      tools?: number;
      source?: string;
    }[];

export interface MCPServerParams {
  name: string;
}

export interface ModelParams {
  model: string;
  provider?: string;
  persist_default: boolean;
}

export interface ModelResult {
  reload_pending?: boolean;
  model: string;
  provider: string;
}

export interface PathParams {
  path: string;
}

export interface PathResult {
  path: string;
}

export interface PermissionConfigureParams {
  external_permissions: boolean;
}

export interface PermissionDecision {
  command_id: string;
  root_id: string;
  permission_id: string;
  allow: boolean;
  reason?: string;
  remember?: string;
}

export interface PermissionDecisionParams {
  decision: unknown;
  signature: string | null;
}

export interface PermissionDecisionResult {
  operation_id: string;
  lease_id: string;
  nonce: string | null;
}

export interface PermissionModeParams {
  command: unknown;
  signature: string | null;
}

export interface PermissionModeResult {
  command: {
    content?: null | {
      reference_id: string;
      digest: string;
      size: string;
      media_type?: string;
      source?: string;
    };
    operation: string;
    result?: unknown;
    failure?: null | {
      data?: null | {
        kind: string;
      };
      code: number;
      message: string;
    };
    command_id: string;
    ingress_seq: string;
    status: string;
  };
  nonce: string | null;
}

export interface PermissionRulesResult {
  rules:
    | null
    | {
        id: string;
        root_id: string;
        operation: string;
        rule: string;
        principal_id: string;
        created_at: string;
      }[];
  global: null | string[];
}

export interface PingResult {
  generation: string;
  build_id: string;
}

export interface ProviderCatalogsResult {
  models: {
    [k: string]: {
      name?: string;
      id?: string;
      providers: null | string[];
      context?: number;
      vision?: boolean;
    };
  };
  providers: {
    [k: string]: {
      base_url: string;
    };
  };
  catalogs: {
    [k: string]: {
      fetched_at: string;
      base_url: string;
      models:
        | null
        | {
            id: string;
            context_length?: number;
            max_completion_tokens?: number;
            reasoning_efforts?: null | string[];
            in_price?: number;
            out_price?: number;
            cache_read_price?: number;
            input_modalities?: null | string[];
          }[];
    };
  };
  errors?: {
    [k: string]: string;
  };
}

export interface ProviderKeySetup {
  revision: string;
  provider: string;
  key?: string;
  environment: boolean;
}

export interface ProviderLoginCreateParams {
  flow_id: string;
  name: string;
}

export interface ProviderLoginList {
  flows:
    | null
    | {
        flow_id: string;
        state: string;
        verification_url?: string;
        user_code?: string;
        email?: string;
        teams:
          | null
          | {
              id: string;
              name: string;
            }[];
        projects:
          | null
          | {
              id: string;
              name: string;
            }[];
        team_id?: string;
        project_id?: string;
        expires_at: string;
        error?: string;
      }[];
}

export interface ProviderLoginParams {
  flow_id: string;
}

export interface ProviderLoginProjectParams {
  flow_id: string;
  project_id: string;
}

export interface ProviderLoginStatus {
  flow_id: string;
  state: string;
  verification_url?: string;
  user_code?: string;
  email?: string;
  teams:
    | null
    | {
        id: string;
        name: string;
      }[];
  projects:
    | null
    | {
        id: string;
        name: string;
      }[];
  team_id?: string;
  project_id?: string;
  expires_at: string;
  error?: string;
}

export interface ProviderLoginTeamParams {
  flow_id: string;
  team_id: string;
}

export interface ProviderNameParams {
  provider: string;
}

export interface ProviderStatus {
  provider: string;
  configured: boolean;
  key_source: string;
  email?: string;
  project_id?: string;
  project_name?: string;
  machine_key_name?: string;
  warnings: null | string[];
}

export interface ProviderValidateParams {
  name: string;
  base_url: string;
  key: string;
}

export interface ProviderValidateResult {
  models:
    | null
    | {
        id: string;
        context_length?: number;
        max_completion_tokens?: number;
        reasoning_efforts?: null | string[];
        pricing?: null | {
          prompt: string;
          completion: string;
          input_cache_read?: string;
        };
        input_modalities?: null | string[];
      }[];
}

export interface QueryParams {
  root_id?: string;
  operation: string;
  payload?: unknown;
}

export interface QueryResult {
  result?: unknown;
  content?: null | {
    reference_id: string;
    digest: string;
    size: string;
    media_type?: string;
    source?: string;
  };
  root_id?: string;
}

export interface QuestionAnswerParams {
  id: string;
  answer: null | string[];
  dismissed: boolean;
}

export interface RPCError {
  data?: null | {
    kind: string;
  };
  code: number;
  message: string;
}

export interface ReplayParams {
  root_id: string;
  cursor: string;
  limit?: number;
}

export interface ReplayResult {
  events:
    | null
    | {
        subscription_id?: string;
        root_id: string;
        seq: string;
        kind: string;
        payload?: unknown;
      }[];
  latest: string;
  expired?: boolean;
}

export interface RestartNotice {
  generation: string;
  cursors: {
    [k: string]: string;
  };
}

export interface RestartParams {
  generation: string;
}

export interface RewindParams {
  expected_revision: string | null;
  cut: number;
}

export interface RewindResult {
  cut: number;
  restored_files: number;
}

export interface RootCollectionPage {
  root_id: string;
  collection: string;
  revision: string;
  event_cursor: string;
  items:
    | null
    | {
        [k: string]: unknown;
      }[];
  next_cursor?: null | {
    root_id: string;
    collection: string;
    revision: string;
    offset: string;
  };
  has_more: boolean;
}

export interface RootCollectionParams {
  root_id: string;
  collection: string;
  cursor?: null | {
    root_id: string;
    collection: string;
    revision: string;
    offset: string;
  };
  limit: number;
  max_bytes: number;
}

export interface RootIDResult {
  root_id: string;
}

export interface RootParams {
  root_id: string;
}

export interface RootSnapshot {
  active_turns: {
    [k: string]: string;
  };
  omitted?: {
    [k: string]: boolean;
  };
  message_seqs: null | number[];
  first_message_seq?: number;
  history_revision: string;
  root_id: string;
  cursor: string;
  meta: {
    id: string;
    kind: string;
    title: string;
    model: string;
    provider: string;
    cwd: string;
    goal: string;
    forked_from: string;
    fork_seq: number;
    tags: null | string[];
    pinned: boolean;
    effort: string;
    usage_in: number;
    usage_cached: number;
    usage_out: number;
    updated_at: string;
  };
  messages:
    | null
    | {
        role: string;
        content: string;
        tool_calls?:
          | null
          | {
              id: string;
              type: string;
              function: {
                name: string;
                arguments: string;
              };
              duration_ms?: number;
              exit_code?: number;
            }[];
        tool_call_id?: string;
        name?: string;
        authored?: boolean;
        sent_at?: null | string;
        usage?: null | {
          prompt_tokens: number;
          completion_tokens: number;
          prompt_tokens_details?: null | {
            cached_tokens: number;
          };
        };
        model?: string;
        rewound_from?: string;
      }[];
  presentation:
    | null
    | {
        seq: string;
        kind: string;
        payload: unknown;
      }[];
  agent_presentations: {
    [k: string]:
      | null
      | {
          seq: string;
          kind: string;
          payload: unknown;
        }[];
  };
  agents:
    | null
    | {
        id: string;
        root_id: string;
        parent_id: string;
        name: string;
        model: string;
        provider: string;
        effort: string;
        cwd: string;
        report: string;
        status: string;
        pending_mail: number;
        lifecycle_phase: string;
        blocking_reason: string;
        terminal_cause: string;
        allowed_controls: null | string[];
      }[];
  inbox:
    | null
    | {
        root_id: string;
        agent_id: string;
        seq: string;
        kind: string;
        status: string;
        payload: {
          inline?: unknown;
          text?: null | string;
          binary?: string | null;
          reference_id: string;
          digest: string;
          size: string;
          media_type: string;
          source: string;
        };
      }[];
  blackboard:
    | null
    | {
        key: string;
        version: string;
        author_agent_id: string;
        payload: {
          inline?: unknown;
          text?: null | string;
          binary?: string | null;
          reference_id: string;
          digest: string;
          size: string;
          media_type: string;
          source: string;
        };
      }[];
  budgets:
    | null
    | {
        agent_id: string;
        state: {
          kind: string;
          limit: string;
          used: string;
          reserved: string;
          remaining: string;
        };
      }[];
  capabilities:
    | null
    | {
        id: string;
        root_id: string;
        agent_id: string;
        issuer_agent_id: string;
        operations: null | string[];
        scopes: null | string[];
        mcp:
          | null
          | {
              server: string;
              tool: string;
              definition: string;
            }[];
        mcp_all: boolean;
        generation: string;
        status: string;
        expires_at: string;
        created_at: string;
        updated_at: string;
      }[];
  schedules:
    | null
    | {
        id: number;
        schedule: string;
        prompt: string;
        anchor: string;
        last_fire: string;
      }[];
  permissions:
    | null
    | {
        id: string;
        agent_id: string;
        operation_id: string;
        operation: string;
        canonical_path: string;
        request_digest: string;
        capability_id: string;
        capability_generation: string;
        status: string;
        command: string;
        rule: string;
      }[];
  questions:
    | null
    | {
        turn_id?: string;
        root_id?: string;
        agent_id?: string;
        sender_agent_id?: string;
        inbox_seq?: string;
        inbox_kind?: string;
        delivery?: string;
        message_id?: string;
        phase?: string;
        status?: string;
        terminal_cause?: string;
        command_client_id?: string;
        command_id?: string;
        operation_id?: string;
        trace_id?: string;
        schedule_id?: number;
        slot?: string;
        error?: string;
        acknowledged_inbox?: string[];
        subscription_id?: string;
        key?: string;
        version?: string;
        expected_version?: string;
        restored?: null | string[];
        not_restored?:
          | null
          | {
              name: string;
              reason: string;
            }[];
        attempt?: string;
        budget_kind?: string;
        amount?: string;
        limit?: string;
        used?: string;
        reserved?: string;
        capability_id?: string;
        generation?: string;
        permission_id?: string;
        operation?: string;
        canonical_path?: string;
        request_digest?: string;
        command?: string;
        rule?: string;
        rule_source?: string;
        question_id?: string;
        question?: string;
        options?:
          | null
          | {
              label: string;
              description?: string;
            }[];
        multiple?: boolean;
        answer?: null | string[];
        dismissed?: boolean;
      }[];
}

export interface RunConfigureParams {
  system?: string;
  max_turns?: number;
  headless?: boolean;
  cache_key?: string;
}

export interface RuntimeConfiguration {
  import_claude: boolean;
  import_codex: boolean;
  revision: string;
  default_model: string;
  default_provider: string;
  default_effort: string;
  compact_model: string;
  compact_provider: string;
  compact_percent: number;
  goal_max_rounds: number;
  max_retries: number;
}

export interface ScheduleCreateParams {
  schedule: string;
  prompt: string;
}

export interface ScheduleDeleteParams {
  schedule_id: number;
}

export type ScheduleListResult =
  | null
  | {
      id: number;
      schedule: string;
      prompt: string;
      anchor: string;
      last_fire: string;
    }[];

export interface ScheduleResult {
  schedule_id: number;
}

export interface SessionCatalogPage {
  revision: string;
  items:
    | null
    | {
        id: string;
        kind: string;
        title: string;
        model: string;
        provider: string;
        cwd: string;
        pinned: boolean;
        updated_at: string;
        truncated: boolean;
      }[];
  next_cursor?: null | {
    revision: string;
    offset: string;
  };
  has_more: boolean;
}

export interface SessionCatalogParams {
  cursor?: null | {
    revision: string;
    offset: string;
  };
  limit: number;
  max_bytes: number;
}

export type SessionListResult =
  | null
  | {
      id: string;
      kind: string;
      title: string;
      model: string;
      provider: string;
      cwd: string;
      goal: string;
      forked_from: string;
      fork_seq: number;
      tags: null | string[];
      pinned: boolean;
      effort: string;
      usage_in: number;
      usage_cached: number;
      usage_out: number;
      updated_at: string;
    }[];

export interface SessionPreviewResult {
  root_id: string;
  user: string;
  assistant: string;
}

export interface SessionUpdateEvent {
  title?: string;
  model?: string;
  provider?: string;
  effort?: string;
  effort_changed?: boolean;
  working_directory?: string;
}

export interface ShellParams {
  command: string;
}

export interface SnapshotParams {
  root_id: string;
}

export interface StreamEvent {
  usage?: null | {
    used: number;
    size: number;
    usage: {
      prompt_tokens: number;
      completion_tokens: number;
      prompt_tokens_details?: null | {
        cached_tokens: number;
      };
    };
  };
  agent_id?: string;
  id?: string;
  name?: string;
  text?: string;
  args?: string;
  result?: string;
}

export interface SubmitPayload {
  text: string;
  parts?:
    | null
    | {
        type: string;
        text?: string;
        image_url?: null | {
          url: string;
        };
        w?: number;
        h?: number;
      }[];
}

export interface SubscribeParams {
  root_id: string;
  subscription_id: string;
  cursor: string;
}

export interface SubscribeResult {
  subscription_id: string;
  cursor: string;
}

export interface SubscriptionFailure {
  subscription_id: string;
  root_id: string;
  error: null | {
    data?: null | {
      kind: string;
    };
    code: number;
    message: string;
  };
}

export interface TerminalInputParams {
  id: string;
  bytes: string | null;
}

export interface TextParams {
  text: string;
}

export interface TextResult {
  text: string;
}

export interface TitleParams {
  title: string;
}

export interface TitleResult {
  title: string;
}

export interface ToolCallParams {
  tool: string;
  arguments: unknown;
}

export interface ToolConfigureParams {
  deny_permissions: boolean;
}

export type ToolSchemaResult =
  | null
  | {
      type: string;
      function: {
        name: string;
        description: string;
        parameters: unknown;
      };
    }[];

export interface UnsubscribeParams {
  subscription_id: string;
}

export interface UploadBeginParams {
  upload_id: string;
  root_id: string;
  expected_digest: string;
  size: string;
  media_type?: string;
  source?: string;
}

export interface UploadChunkParams {
  upload_id: string;
  offset: string;
  data: string | null;
}

export interface UploadFinishParams {
  upload_id: string;
}

export type UserHistoryResult = null | string[];

export interface ContractTypes {
  Accepted: Accepted;
  AgentCancelParams: AgentCancelParams;
  AgentInputParams: AgentInputParams;
  AgentListResult: AgentListResult;
  AgentSubmitResult: AgentSubmitResult;
  AgentTranscriptResult: AgentTranscriptResult;
  BoundedTranscriptPage: BoundedTranscriptPage;
  BrowserDriverParams: BrowserDriverParams;
  BrowserStatusResult: BrowserStatusResult;
  BudgetCapParams: BudgetCapParams;
  BudgetState: BudgetState;
  CancelParams: CancelParams;
  CapabilityRecord: CapabilityRecord;
  CatalogRevision: CatalogRevision;
  CheckpointParams: CheckpointParams;
  CommandParams: CommandParams;
  CommandResult: CommandResult;
  CommandStatusParams: CommandStatusParams;
  CompactionListResult: CompactionListResult;
  CompactionParams: CompactionParams;
  CompactionResult: CompactionResult;
  CompactionRetryResult: CompactionRetryResult;
  CompactionSettingsResult: CompactionSettingsResult;
  CompletionParams: CompletionParams;
  CompletionResult: CompletionResult;
  ComputerAppParams: ComputerAppParams;
  ComputerStatusResult: ComputerStatusResult;
  ConfigurationUpdate: ConfigurationUpdate;
  ContentEventPayload: ContentEventPayload;
  ContentHandle: ContentHandle;
  ContentReadParams: ContentReadParams;
  ContentReadResult: ContentReadResult;
  ContextAuditResult: ContextAuditResult;
  CreateSessionParams: CreateSessionParams;
  EffortParams: EffortParams;
  EffortResult: EffortResult;
  Empty: Empty;
  EmptyParams: EmptyParams;
  EnrollIdentityParams: EnrollIdentityParams;
  EventNotification: EventNotification;
  ForkParams: ForkParams;
  GoalContextParams: GoalContextParams;
  GoalResult: GoalResult;
  HistoryPageParams: HistoryPageParams;
  IDParams: IDParams;
  IdentityResult: IdentityResult;
  IdentityStatusResult: IdentityStatusResult;
  InitializeParams: InitializeParams;
  InitializeResult: InitializeResult;
  LSPListResult: LSPListResult;
  LifecycleEvent: LifecycleEvent;
  ListParams: ListParams;
  MCPAttachParams: MCPAttachParams;
  MCPImportParams: MCPImportParams;
  MCPImportStatusResult: MCPImportStatusResult;
  MCPListResult: MCPListResult;
  MCPServerParams: MCPServerParams;
  ModelParams: ModelParams;
  ModelResult: ModelResult;
  PathParams: PathParams;
  PathResult: PathResult;
  PermissionConfigureParams: PermissionConfigureParams;
  PermissionDecision: PermissionDecision;
  PermissionDecisionParams: PermissionDecisionParams;
  PermissionDecisionResult: PermissionDecisionResult;
  PermissionModeParams: PermissionModeParams;
  PermissionModeResult: PermissionModeResult;
  PermissionRulesResult: PermissionRulesResult;
  PingResult: PingResult;
  ProviderCatalogsResult: ProviderCatalogsResult;
  ProviderKeySetup: ProviderKeySetup;
  ProviderLoginCreateParams: ProviderLoginCreateParams;
  ProviderLoginList: ProviderLoginList;
  ProviderLoginParams: ProviderLoginParams;
  ProviderLoginProjectParams: ProviderLoginProjectParams;
  ProviderLoginStatus: ProviderLoginStatus;
  ProviderLoginTeamParams: ProviderLoginTeamParams;
  ProviderNameParams: ProviderNameParams;
  ProviderStatus: ProviderStatus;
  ProviderValidateParams: ProviderValidateParams;
  ProviderValidateResult: ProviderValidateResult;
  QueryParams: QueryParams;
  QueryResult: QueryResult;
  QuestionAnswerParams: QuestionAnswerParams;
  RPCError: RPCError;
  ReplayParams: ReplayParams;
  ReplayResult: ReplayResult;
  RestartNotice: RestartNotice;
  RestartParams: RestartParams;
  RewindParams: RewindParams;
  RewindResult: RewindResult;
  RootCollectionPage: RootCollectionPage;
  RootCollectionParams: RootCollectionParams;
  RootIDResult: RootIDResult;
  RootParams: RootParams;
  RootSnapshot: RootSnapshot;
  RunConfigureParams: RunConfigureParams;
  RuntimeConfiguration: RuntimeConfiguration;
  ScheduleCreateParams: ScheduleCreateParams;
  ScheduleDeleteParams: ScheduleDeleteParams;
  ScheduleListResult: ScheduleListResult;
  ScheduleResult: ScheduleResult;
  SessionCatalogPage: SessionCatalogPage;
  SessionCatalogParams: SessionCatalogParams;
  SessionListResult: SessionListResult;
  SessionPreviewResult: SessionPreviewResult;
  SessionUpdateEvent: SessionUpdateEvent;
  ShellParams: ShellParams;
  SnapshotParams: SnapshotParams;
  StreamEvent: StreamEvent;
  SubmitPayload: SubmitPayload;
  SubscribeParams: SubscribeParams;
  SubscribeResult: SubscribeResult;
  SubscriptionFailure: SubscriptionFailure;
  TerminalInputParams: TerminalInputParams;
  TextParams: TextParams;
  TextResult: TextResult;
  TitleParams: TitleParams;
  TitleResult: TitleResult;
  ToolCallParams: ToolCallParams;
  ToolConfigureParams: ToolConfigureParams;
  ToolSchemaResult: ToolSchemaResult;
  UnsubscribeParams: UnsubscribeParams;
  UploadBeginParams: UploadBeginParams;
  UploadChunkParams: UploadChunkParams;
  UploadFinishParams: UploadFinishParams;
  UserHistoryResult: UserHistoryResult;
}
export interface EventPayloadTypes {
  "agent.admitted": LifecycleEvent | ContentEventPayload;
  "agent.prompt.queued": LifecycleEvent | ContentEventPayload;
  "agent.subtree.deleted": LifecycleEvent | ContentEventPayload;
  "agent.subtree.stopped": LifecycleEvent | ContentEventPayload;
  "agent.turn.cancelled": LifecycleEvent | ContentEventPayload;
  "agent.turn.failed": LifecycleEvent | ContentEventPayload;
  "agent.turn.interrupted": LifecycleEvent | ContentEventPayload;
  "agent.turn.started": LifecycleEvent | ContentEventPayload;
  "agent.turn.succeeded": LifecycleEvent | ContentEventPayload;
  "blackboard.append": LifecycleEvent | ContentEventPayload;
  "blackboard.cas": LifecycleEvent | ContentEventPayload;
  "blackboard.set": LifecycleEvent | ContentEventPayload;
  "budget.active_child.reserved": LifecycleEvent | ContentEventPayload;
  "budget.capped": LifecycleEvent | ContentEventPayload;
  "capability.delegated": LifecycleEvent | ContentEventPayload;
  "capability.revoked": LifecycleEvent | ContentEventPayload;
  "command.cancelled": LifecycleEvent | ContentEventPayload;
  "command.control.queued": LifecycleEvent | ContentEventPayload;
  "command.failed": LifecycleEvent | ContentEventPayload;
  "command.interrupted": LifecycleEvent | ContentEventPayload;
  "command.queued": LifecycleEvent | ContentEventPayload;
  "command.running": LifecycleEvent | ContentEventPayload;
  "command.succeeded": LifecycleEvent | ContentEventPayload;
  "command.waiting": LifecycleEvent | ContentEventPayload;
  "goal.continued": LifecycleEvent | ContentEventPayload;
  "inbox.consumed": LifecycleEvent | ContentEventPayload;
  "inbox.failed": LifecycleEvent | ContentEventPayload;
  "inbox.queued": LifecycleEvent | ContentEventPayload;
  "message.deferred": LifecycleEvent | ContentEventPayload;
  "message.delivered": LifecycleEvent | ContentEventPayload;
  "message.done": LifecycleEvent | ContentEventPayload;
  "message.queued": LifecycleEvent | ContentEventPayload;
  "message.updated": LifecycleEvent | ContentEventPayload;
  "permission.auto_approved": LifecycleEvent | ContentEventPayload;
  "permission.pending": LifecycleEvent | ContentEventPayload;
  "question.answered": LifecycleEvent | ContentEventPayload;
  "question.closed": LifecycleEvent | ContentEventPayload;
  "question.pending": LifecycleEvent | ContentEventPayload;
  "root.failed": LifecycleEvent | ContentEventPayload;
  "root.interrupted": LifecycleEvent | ContentEventPayload;
  "root.stopped": LifecycleEvent | ContentEventPayload;
  "schedule.fired": LifecycleEvent | ContentEventPayload;
  "scratch.restored": LifecycleEvent | ContentEventPayload;
  "session.cwd.updated": SessionUpdateEvent | ContentEventPayload;
  "session.effort.updated": SessionUpdateEvent | ContentEventPayload;
  "session.model.updated": SessionUpdateEvent | ContentEventPayload;
  "session.reload.failed": LifecycleEvent | ContentEventPayload;
  "session.title.updated": SessionUpdateEvent | ContentEventPayload;
  "state.private.append": LifecycleEvent | ContentEventPayload;
  "state.private.cas": LifecycleEvent | ContentEventPayload;
  "state.private.set": LifecycleEvent | ContentEventPayload;
  "stream.cell.host": StreamEvent | ContentEventPayload;
  "stream.notice": StreamEvent | ContentEventPayload;
  "stream.reasoning": StreamEvent | ContentEventPayload;
  "stream.terminal.awaiting": StreamEvent | ContentEventPayload;
  "stream.terminal.completed": StreamEvent | ContentEventPayload;
  "stream.terminal.output": StreamEvent | ContentEventPayload;
  "stream.terminal.started": StreamEvent | ContentEventPayload;
  "stream.text": StreamEvent | ContentEventPayload;
  "stream.tool.call": StreamEvent | ContentEventPayload;
  "stream.tool.completed": StreamEvent | ContentEventPayload;
  "stream.tool.output": StreamEvent | ContentEventPayload;
  "stream.tool.started": StreamEvent | ContentEventPayload;
  "stream.usage": StreamEvent | ContentEventPayload;
  "subscription.cancelled": LifecycleEvent | ContentEventPayload;
  "subscription.created": LifecycleEvent | ContentEventPayload;
  "turn.cancelled": LifecycleEvent | ContentEventPayload;
  "turn.failed": LifecycleEvent | ContentEventPayload;
  "turn.interrupted": LifecycleEvent | ContentEventPayload;
  "turn.started": LifecycleEvent | ContentEventPayload;
  "turn.succeeded": LifecycleEvent | ContentEventPayload;
}
export interface RpcMethods {
  "command.status": { params: CommandStatusParams; result: CommandResult; execution: "query"; permission: "client-command-namespace"; sensitive: false };
  "command.submit": { params: CommandParams; result: CommandResult; execution: "command"; permission: "operation-specific"; sensitive: false };
  "config.get": { params: Empty; result: RuntimeConfiguration; execution: "query"; permission: "none"; sensitive: false };
  "config.update": { params: ConfigurationUpdate; result: RuntimeConfiguration; execution: "ephemeral"; permission: "configuration-revision"; sensitive: false };
  "content.read": { params: ContentReadParams; result: ContentReadResult; execution: "query"; permission: "root-agent-content-grant"; sensitive: false };
  "daemon.ping": { params: Empty; result: PingResult; execution: "query"; permission: "none"; sensitive: false };
  "daemon.restart": { params: RestartParams; result: Empty; execution: "lifecycle"; permission: "armed-generation"; sensitive: false };
  "daemon.stop": { params: RestartParams; result: Empty; execution: "lifecycle"; permission: "armed-generation"; sensitive: false };
  "events.replay": { params: ReplayParams; result: ReplayResult; execution: "query"; permission: "root-association"; sensitive: false };
  "events.subscribe": { params: SubscribeParams; result: SubscribeResult; execution: "subscription"; permission: "root-association"; sensitive: false };
  "events.unsubscribe": { params: UnsubscribeParams; result: Empty; execution: "subscription"; permission: "connection-subscription"; sensitive: false };
  "history.page": { params: HistoryPageParams; result: BoundedTranscriptPage; execution: "query"; permission: "root-agent-association"; sensitive: false };
  "identity.enroll": { params: EnrollIdentityParams; result: IdentityResult; execution: "ephemeral"; permission: "existing-human-enrollment"; sensitive: true };
  "identity.status": { params: Empty; result: IdentityStatusResult; execution: "query"; permission: "none"; sensitive: false };
  "initialize": { params: InitializeParams; result: InitializeResult; execution: "query"; permission: "none"; sensitive: false };
  "operation.invoke": { params: QueryParams; result: QueryResult; execution: "ephemeral"; permission: "operation-specific"; sensitive: true };
  "permission.decide": { params: PermissionDecisionParams; result: PermissionDecisionResult; execution: "ephemeral"; permission: "signed-human-decision"; sensitive: true };
  "permission.mode": { params: PermissionModeParams; result: PermissionModeResult; execution: "ephemeral"; permission: "signed-human-mode"; sensitive: true };
  "provider.key.rotate": { params: ProviderNameParams; result: ProviderStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: true };
  "provider.key.set": { params: ProviderKeySetup; result: RuntimeConfiguration; execution: "ephemeral"; permission: "configuration-revision"; sensitive: true };
  "provider.login.begin": { params: Empty; result: ProviderLoginStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.login.cancel": { params: ProviderLoginParams; result: ProviderLoginStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.login.list": { params: Empty; result: ProviderLoginList; execution: "query"; permission: "host-configuration"; sensitive: false };
  "provider.login.project.create": { params: ProviderLoginCreateParams; result: ProviderLoginStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.login.project.select": { params: ProviderLoginProjectParams; result: ProviderLoginStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.login.status": { params: ProviderLoginParams; result: ProviderLoginStatus; execution: "query"; permission: "none"; sensitive: false };
  "provider.login.team.select": { params: ProviderLoginTeamParams; result: ProviderLoginStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.logout": { params: ProviderNameParams; result: ProviderStatus; execution: "ephemeral"; permission: "host-configuration"; sensitive: false };
  "provider.status": { params: ProviderNameParams; result: ProviderStatus; execution: "query"; permission: "host-configuration"; sensitive: false };
  "provider.validate": { params: ProviderValidateParams; result: ProviderValidateResult; execution: "ephemeral"; permission: "host-configuration"; sensitive: true };
  "query": { params: QueryParams; result: QueryResult; execution: "query"; permission: "operation-specific"; sensitive: false };
  "root.collection": { params: RootCollectionParams; result: RootCollectionPage; execution: "query"; permission: "root-association"; sensitive: false };
  "root.snapshot": { params: SnapshotParams; result: RootSnapshot; execution: "query"; permission: "root-association"; sensitive: false };
  "sessions.list": { params: SessionCatalogParams; result: SessionCatalogPage; execution: "query"; permission: "host-runtime"; sensitive: false };
  "sessions.revision": { params: EmptyParams; result: CatalogRevision; execution: "query"; permission: "host-runtime"; sensitive: false };
  "upload.begin": { params: UploadBeginParams; result: Accepted; execution: "ephemeral"; permission: "content-grant"; sensitive: false };
  "upload.chunk": { params: UploadChunkParams; result: Accepted; execution: "ephemeral"; permission: "connection-upload"; sensitive: false };
  "upload.finish": { params: UploadFinishParams; result: ContentHandle; execution: "ephemeral"; permission: "content-grant"; sensitive: false };
  "workspace.complete": { params: CompletionParams; result: CompletionResult; execution: "query"; permission: "root-agent-association"; sensitive: false };
}
export interface RuntimeOperations {
  "agent.control": { params: IDParams; result: Empty; execution: "command"; permission: "agent-authority"; sensitive: false };
  "agent.delete": { params: IDParams; result: Empty; execution: "command"; permission: "agent-authority"; sensitive: false };
  "agent.submit": { params: AgentInputParams; result: AgentSubmitResult; execution: "command"; permission: "agent-admission"; sensitive: false };
  "agent.transcript": { params: IDParams; result: AgentTranscriptResult; execution: "query"; permission: "human-transcript-inspection"; sensitive: false };
  "agent.turn.cancel": { params: AgentCancelParams; result: Empty; execution: "command"; permission: "target-turn"; sensitive: false };
  "agents.list": { params: EmptyParams; result: AgentListResult; execution: "query"; permission: "root-association"; sensitive: false };
  "browser.set_driver": { params: BrowserDriverParams; result: BrowserStatusResult; execution: "command"; permission: "host-configuration"; sensitive: false };
  "browser.status": { params: EmptyParams; result: BrowserStatusResult; execution: "query"; permission: "root-association"; sensitive: false };
  "budget.cap": { params: BudgetCapParams; result: BudgetState; execution: "command"; permission: "budget-authority"; sensitive: false };
  "cancel": { params: CancelParams; result: Empty; execution: "command"; permission: "target-turn"; sensitive: false };
  "capability.revoke": { params: IDParams; result: CapabilityRecord; execution: "command"; permission: "capability-authority"; sensitive: false };
  "compaction.configure": { params: CompactionParams; result: CompactionSettingsResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "computer.allow": { params: ComputerAppParams; result: ComputerStatusResult; execution: "command"; permission: "human-computer-policy"; sensitive: false };
  "computer.deny": { params: ComputerAppParams; result: ComputerStatusResult; execution: "command"; permission: "human-computer-policy"; sensitive: false };
  "computer.status": { params: EmptyParams; result: ComputerStatusResult; execution: "query"; permission: "root-association"; sensitive: false };
  "context.audit": { params: EmptyParams; result: ContextAuditResult; execution: "query"; permission: "root-association"; sensitive: false };
  "daemon.checkpoint": { params: CheckpointParams; result: RestartNotice; execution: "command"; permission: "host-runtime"; sensitive: false };
  "goal.from-context": { params: GoalContextParams; result: GoalResult; execution: "command"; permission: "root-admission"; sensitive: false };
  "goal.run": { params: TextParams; result: GoalResult; execution: "command"; permission: "root-admission"; sensitive: false };
  "goal.set": { params: TextParams; result: GoalResult; execution: "command"; permission: "root-admission"; sensitive: false };
  "history.clear": { params: EmptyParams; result: Empty; execution: "command"; permission: "root-idle"; sensitive: false };
  "history.compact": { params: EmptyParams; result: CompactionResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "history.compact.log": { params: EmptyParams; result: CompactionListResult; execution: "query"; permission: "root-association"; sensitive: false };
  "history.compact.retry": { params: EmptyParams; result: CompactionRetryResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "history.rewind": { params: RewindParams; result: RewindResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "history.user.list": { params: EmptyParams; result: UserHistoryResult; execution: "query"; permission: "root-association"; sensitive: false };
  "lsp.status": { params: EmptyParams; result: LSPListResult; execution: "query"; permission: "root-association"; sensitive: false };
  "mcp.attach": { params: MCPAttachParams; result: Empty; execution: "ephemeral"; permission: "delegated-mcp-authority"; sensitive: true };
  "mcp.disable": { params: MCPServerParams; result: Empty; execution: "command"; permission: "delegated-mcp-authority"; sensitive: false };
  "mcp.enable": { params: MCPServerParams; result: Empty; execution: "command"; permission: "delegated-mcp-authority"; sensitive: false };
  "mcp.import.configure": { params: MCPImportParams; result: MCPImportStatusResult; execution: "command"; permission: "host-configuration"; sensitive: false };
  "mcp.import.status": { params: EmptyParams; result: MCPImportStatusResult; execution: "query"; permission: "host-configuration"; sensitive: false };
  "mcp.reconnect": { params: MCPServerParams; result: Empty; execution: "command"; permission: "delegated-mcp-authority"; sensitive: false };
  "mcp.status": { params: EmptyParams; result: MCPListResult; execution: "query"; permission: "root-association"; sensitive: false };
  "permission.forget": { params: IDParams; result: Empty; execution: "command"; permission: "rule-authority"; sensitive: false };
  "permission.mode": { params: PermissionConfigureParams; result: Empty; execution: "command"; permission: "signed-human-if-disabling"; sensitive: false };
  "permission.rules": { params: EmptyParams; result: PermissionRulesResult; execution: "query"; permission: "root-association"; sensitive: false };
  "provider.catalogs": { params: EmptyParams; result: ProviderCatalogsResult; execution: "query"; permission: "host-runtime"; sensitive: false };
  "question.answer": { params: QuestionAnswerParams; result: Empty; execution: "command"; permission: "pending-question"; sensitive: false };
  "run.configure": { params: RunConfigureParams; result: Empty; execution: "command"; permission: "root-idle"; sensitive: false };
  "schedule.create": { params: ScheduleCreateParams; result: ScheduleResult; execution: "command"; permission: "schedule-budget"; sensitive: false };
  "schedule.delete": { params: ScheduleDeleteParams; result: ScheduleResult; execution: "command"; permission: "root-association"; sensitive: false };
  "schedule.list": { params: EmptyParams; result: ScheduleListResult; execution: "query"; permission: "root-association"; sensitive: false };
  "session.autotitle": { params: EmptyParams; result: Empty; execution: "command"; permission: "root-association"; sensitive: false };
  "session.create": { params: CreateSessionParams; result: RootIDResult; execution: "command"; permission: "host-runtime"; sensitive: false };
  "session.delete": { params: RootParams; result: RootIDResult; execution: "command"; permission: "root-association"; sensitive: false };
  "session.effort": { params: EffortParams; result: EffortResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "session.effort.get": { params: EmptyParams; result: EffortResult; execution: "query"; permission: "root-association"; sensitive: false };
  "session.fork": { params: ForkParams; result: RootIDResult; execution: "command"; permission: "root-association"; sensitive: false };
  "session.list": { params: ListParams; result: SessionListResult; execution: "query"; permission: "host-runtime"; sensitive: false };
  "session.model": { params: ModelParams; result: ModelResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "session.model.get": { params: EmptyParams; result: ModelResult; execution: "query"; permission: "root-association"; sensitive: false };
  "session.open": { params: IDParams; result: RootIDResult; execution: "query"; permission: "root-association"; sensitive: false };
  "session.preview": { params: IDParams; result: SessionPreviewResult; execution: "query"; permission: "root-association"; sensitive: false };
  "session.reload": { params: EmptyParams; result: ModelResult; execution: "command"; permission: "root-idle"; sensitive: false };
  "session.rename": { params: TitleParams; result: TitleResult; execution: "command"; permission: "root-association"; sensitive: false };
  "shell.run": { params: ShellParams; result: TextResult; execution: "command"; permission: "tool-permissions"; sensitive: false };
  "steer": { params: SubmitPayload; result: TextResult; execution: "command"; permission: "root-admission"; sensitive: false };
  "submit": { params: SubmitPayload; result: TextResult; execution: "command"; permission: "root-admission"; sensitive: false };
  "terminal.input": { params: TerminalInputParams; result: Empty; execution: "ephemeral"; permission: "active-terminal"; sensitive: true };
  "tool.call": { params: ToolCallParams; result: TextResult; execution: "command"; permission: "tool-permissions"; sensitive: false };
  "tool.configure": { params: ToolConfigureParams; result: Empty; execution: "command"; permission: "tool-permissions"; sensitive: false };
  "tool.schema": { params: EmptyParams; result: ToolSchemaResult; execution: "query"; permission: "tool-authority"; sensitive: false };
  "workspace.inspect": { params: EmptyParams; result: PathResult; execution: "query"; permission: "root-association"; sensitive: false };
  "workspace.set": { params: PathParams; result: PathResult; execution: "command"; permission: "workspace-authority"; sensitive: false };
}

export type RpcMethod = keyof RpcMethods;
export type RuntimeOperation = keyof RuntimeOperations;
export type RuntimeOperationOf<E extends RuntimeOperations[RuntimeOperation]['execution']> = {
  [K in RuntimeOperation]: RuntimeOperations[K]['execution'] extends E ? K : never
}[RuntimeOperation];
export type QueryOperation = RuntimeOperationOf<'query'>;
export type CommandOperation = RuntimeOperationOf<'command'>;
export type EphemeralOperation = RuntimeOperationOf<'ephemeral'>;
export interface OperationMetadata {
  readonly name: string;
  readonly surface: 'rpc' | 'runtime';
  readonly execution: 'query' | 'command' | 'ephemeral' | 'subscription' | 'lifecycle';
  readonly permission: string;
  readonly sensitive: boolean;
  readonly params_type: keyof ContractTypes;
  readonly result_type: keyof ContractTypes;
}
export declare const rpcOperations: Readonly<{ [K in RpcMethod]: OperationMetadata & { readonly name: K; readonly execution: RpcMethods[K]['execution']; readonly permission: RpcMethods[K]['permission']; readonly sensitive: RpcMethods[K]['sensitive'] } }>;
export declare const runtimeOperations: Readonly<{ [K in RuntimeOperation]: OperationMetadata & { readonly name: K; readonly execution: RuntimeOperations[K]['execution']; readonly permission: RuntimeOperations[K]['permission']; readonly sensitive: RuntimeOperations[K]['sensitive'] } }>;
export type RootEventEnvelope = Omit<EventNotification['event'], 'kind' | 'payload'>;
export type TypedRootEvent = { [K in keyof EventPayloadTypes]: RootEventEnvelope & { unknown?: false; kind: K; payload: EventPayloadTypes[K] } }[keyof EventPayloadTypes];
export type UnknownRootEvent = RootEventEnvelope & { unknown: true; kind: string; payload: unknown };
export type RootEvent = TypedRootEvent | UnknownRootEvent;
export type TypedEventNotification = { event: RootEvent };
export type ValidationMode = 'request' | 'response';
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown, mode?: ValidationMode): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown, mode?: ValidationMode): asserts value is ContractTypes[T];
export declare const manifest: {
  major: number; minor: number;
  operations: readonly (Omit<OperationMetadata, 'sensitive'> & { readonly sensitive?: boolean })[];
  events: Readonly<Record<string, keyof ContractTypes>>;
  event_payloads: Readonly<Record<string, keyof ContractTypes>>;
};
