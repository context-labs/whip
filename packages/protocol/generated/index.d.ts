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
export type TypedRootEvent = { [K in keyof EventPayloadTypes]: { kind: K; payload: EventPayloadTypes[K] } }[keyof EventPayloadTypes];
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];
export declare const manifest: {
  major: number; minor: number;
  operations: readonly { name: string; surface: string; execution: string; permission: string; sensitive?: boolean; params_type: keyof ContractTypes; result_type: keyof ContractTypes }[];
  events: Readonly<Record<string, keyof ContractTypes>>;
 event_payloads: Readonly<Record<string, keyof ContractTypes>>;
};
