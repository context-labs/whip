// Generated from Go DTOs. Run npm run generate.
export interface Admission {
  receipt: {
    identity: {
      client_id: string;
      request_id: string;
    };
    digest: string;
    input_id: null | string;
    deleted_at: null | string;
    created_at: string;
  };
  input:
    | {
        host_operation: null;
        goal: null | {
          id: string;
          revision: string;
        };
        id: string;
        session_id: string;
        source: "user" | "agent" | "schedule" | "goal";
        kind: "prompt";
        /**
         * @minItems 1
         * @maxItems 128
         */
        parts: [
          (
            | {
                text: string;
                type: "text";
              }
            | {
                reference_id: string;
                type: "content";
              }
          ),
          ...(
            | {
                text: string;
                type: "text";
              }
            | {
                reference_id: string;
                type: "content";
              }
          )[]
        ];
        state: "queued" | "claimed" | "cancelled";
        turn_id: null | string;
        created_at: string;
        schedule: null | {
          schedule_id: string;
          scheduled_for: string;
        };
      }
    | {
        host_operation: null;
        goal: null | {
          id: string;
          revision: string;
        };
        id: string;
        session_id: string;
        source: "user" | "agent" | "schedule" | "goal";
        kind: "compact" | "goal_formulation" | "automatic_title";
        /**
         * @maxItems 0
         */
        parts: [];
        state: "queued" | "claimed" | "cancelled";
        turn_id: null | string;
        created_at: string;
        schedule: null | {
          schedule_id: string;
          scheduled_for: string;
        };
      }
    | {
        host_operation: {
          module: "shell" | "files" | "tools" | "computer";
          name: string;
          arguments_base64: string;
        };
        goal: null | {
          id: string;
          revision: string;
        };
        id: string;
        session_id: string;
        source: "user";
        kind: "host_operation";
        /**
         * @maxItems 0
         */
        parts: [];
        state: "queued" | "claimed" | "cancelled";
        turn_id: null | string;
        created_at: string;
        schedule: null | {
          schedule_id: string;
          scheduled_for: string;
        };
      }
    | null;
  turn: null | {
    history_revision: string;
    goal: null | {
      id: string;
      revision: string;
    };
    id: string;
    session_id: string;
    kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
    config_revision: string;
    state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
    failure: null | string;
    started_at: string;
    finished_at: null | string;
  };
}
export interface AnswerQuestionParams {
  session_id: string;
  operation_id: string;
  /**
   * @minItems 1
   * @maxItems 8
   */
  answers: [
    {
      /**
       * @minItems 0
       * @maxItems 7
       */
      answer: string[];
      dismissed: boolean;
    },
    ...{
      /**
       * @minItems 0
       * @maxItems 7
       */
      answer: string[];
      dismissed: boolean;
    }[]
  ];
}
export interface AutomaticTitleDecision {
  tree_id: string;
  session_id: string;
  input_id: null | string;
  receipt_identity: null | {
    client_id: string;
    request_id: string;
  };
  config_revision: string;
  expected_revision: string;
  enabled: boolean;
  model: {
    provider: string;
    name: string;
    effort: string;
    temperature?: null | number;
    top_p?: null | number;
  };
  source: string;
  reason: "eligible" | "disabled" | "short" | "manual" | "ineligible" | "fork";
  created_at: string;
}
export interface AutomaticTitleResult {
  tree_id: string;
  attempt_id: string;
  text: string;
  applied: boolean;
  created_at: string;
}
export interface AutomaticTitleResultParams {
  tree_id: string;
  attempt_id: string;
}
export interface Budget {
  session_id: string;
  kind:
    | "model_calls"
    | "model_tokens"
    | "model_cost_nano_usd"
    | "model_elapsed_millis"
    | "logical_writes"
    | "logical_write_bytes";
  revision: string;
  limit: null | string;
  used: string;
  reserved: string;
  uncertain: string;
  incomplete: boolean;
}
export interface BudgetsResult {
  items:
    | null
    | {
        session_id: string;
        kind:
          | "model_calls"
          | "model_tokens"
          | "model_cost_nano_usd"
          | "model_elapsed_millis"
          | "logical_writes"
          | "logical_write_bytes";
        revision: string;
        limit: null | string;
        used: string;
        reserved: string;
        uncertain: string;
        incomplete: boolean;
      }[];
}
export interface CallHostToolParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
  operation: {
    module: "shell" | "files" | "tools" | "computer";
    name: string;
    arguments_base64: string;
  };
}
export interface Cell {
  id: string;
  session_id: string;
  turn_id: string;
  call_message_id: string;
  call_id: string;
  state: "running" | "succeeded" | "failed" | "uncertain";
  result_message_id: null | string;
  checkpoint: null | {
    digest: string;
    size: string;
    engine: "starlark" | "quickjs";
    metadata: unknown;
  };
  created_at: string;
  finished_at: null | string;
}
export interface CellParams {
  cell_id: string;
}
export interface CellsParams {
  turn_id: string;
  after?: null | string;
  limit: number;
}
export interface CellsResult {
  items:
    | null
    | {
        id: string;
        session_id: string;
        turn_id: string;
        call_message_id: string;
        call_id: string;
        state: "running" | "succeeded" | "failed" | "uncertain";
        result_message_id: null | string;
        checkpoint: null | {
          digest: string;
          size: string;
          engine: "starlark" | "quickjs";
          metadata: unknown;
        };
        created_at: string;
        finished_at: null | string;
      }[];
}
export interface ChangeProviderParams {
  revision: string;
  provider: string;
  declaration: {
    kind: "openai-chat" | "openai-responses" | "openai-codex";
    base_url: string;
    credential: null | {
      source: "env" | "file" | "command" | "none" | "inference-net";
      environment: string;
      file: string;
      command: null | {
        executable: string;
        /**
         * @maxItems 64
         */
        arguments: string[];
        /**
         * @maxItems 64
         */
        environment: string[];
      };
    };
    models: {
      [k: string]: {
        prices: {
          input: null | string;
          output: null | string;
          reasoning: null | string;
          cached_input: null | string;
          cached_output: null | string;
        };
        context_window_tokens: null | string;
        max_output_tokens: string;
        timeout_millis: string;
        max_attempts: number;
      };
    } | null;
  };
  keep_credential: boolean;
  key: null | {
    id: string;
    key: string;
  };
}
export interface CompactParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
}
export interface CompactionParams {
  session_id: string;
  compaction_id: string;
}
export interface CompactionResult {
  metadata: {
    history_revision: string;
    source: null | {
      session_id: string;
      compaction_id: string;
    };
    id: string;
    session_id: string;
    turn_id: null | string;
    attempt_id: null | string;
    base_id: null | string;
    expected_revision: string;
    through_sequence: string;
    pinned_message_ids: null | string[];
    text_bytes: string;
    created_at: string;
  };
  text: string;
}
export interface CompactionsParams {
  session_id: string;
  after?: null | string;
  limit: number;
}
export interface CompactionsResult {
  items:
    | null
    | {
        history_revision: string;
        source: null | {
          session_id: string;
          compaction_id: string;
        };
        id: string;
        session_id: string;
        turn_id: null | string;
        attempt_id: null | string;
        base_id: null | string;
        expected_revision: string;
        through_sequence: string;
        pinned_message_ids: null | string[];
        text_bytes: string;
        created_at: string;
      }[];
}
export interface ComputerConnectionParams {
  generation: string;
}
export interface ComputerStatus {
  revision: string;
  configuration: {
    enabled: boolean;
    helper_executable: string;
    /**
     * @maxItems 64
     */
    allow: string[];
    /**
     * @maxItems 64
     */
    deny: string[];
    default_deny: boolean;
  };
  generation: string;
  state: "disabled" | "available" | "connected" | "retired" | "closed";
  native_configured: boolean;
  platform_supported: boolean;
}
export interface ConfigureComputerParams {
  revision: string;
  configuration: {
    enabled: boolean;
    helper_executable: string;
    /**
     * @maxItems 64
     */
    allow: string[];
    /**
     * @maxItems 64
     */
    deny: string[];
    default_deny: boolean;
  };
}
export interface ConfigureMCPParams {
  revision: string;
  name: string;
  server: null | {
    /**
     * @maxItems 128
     */
    command: string[];
    env: {
      [k: string]: string;
    };
    cwd: string;
    url: string;
    headers: {
      [k: string]: string;
    };
    enabled: null | boolean;
    note: string;
    startup_timeout_seconds: number;
    tool_timeout_seconds: number;
  };
  remove: boolean;
  imports: null | {
    claude: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    codex: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    project: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    opencode: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    offered: boolean;
  };
  brand_icons: null | boolean;
}
export interface ContentReference {
  id: string;
  session_id: string;
  digest: string;
  size: string;
  media_type: string;
  created_at: string;
}
export interface ContextHead {
  session_id: string;
  revision: string;
  compaction_id: null | string;
}
export interface ContextHistoryParams {
  expected_revision?: null | string;
  session_id: string;
  after: string;
  through_sequence: string;
  limit: number;
}
export interface CreateGoalParams {
  session_id: string;
  goal_id: string;
  expected_current: null | {
    id: string;
    revision: string;
  };
  spec: {
    text: string;
    max_continuations?: null | string;
  };
  start: boolean;
}
export interface CreateGrantParams {
  id: string;
  session_id: string;
  capability: string;
  resource: string;
  issuer_id?: null | string;
}
export interface CreateScheduleParams {
  session_id: string;
  schedule_id: string;
  expression: string;
  /**
   * @minItems 1
   * @maxItems 128
   */
  parts: [
    (
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
      | {
          call: {
            arguments: {
              [k: string]: unknown;
            };
            id: string;
            name: string;
          };
          type: "tool_call";
        }
      | {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        }
    ),
    ...(
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
      | {
          call: {
            arguments: {
              [k: string]: unknown;
            };
            id: string;
            name: string;
          };
          type: "tool_call";
        }
      | {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        }
    )[]
  ];
}
export interface CreateTreeParams {
  creation_id: string;
  permission_mode?: null | ("prompt" | "automatic");
  metadata: {
    title: null | string;
    archived: boolean;
    pinned: boolean;
  };
  engine: "starlark" | "quickjs";
  resources?:
    | null
    | {
        kind:
          | "depth"
          | "descendants"
          | "queued_inputs"
          | "active_operations"
          | "subscriptions"
          | "runnable_descendants"
          | "schedules";
        limit: null | string;
      }[];
  definition: {
    id: string;
    revision: string;
  };
  overrides: {
    mcp_servers?: {
      [k: string]: unknown;
    } & (null | {
      all: boolean;
      /**
       * @maxItems 64
       */
      servers: string[];
    });
    /**
     * @maxItems 17
     */
    modules?:
      | null
      | (
          | "agents"
          | "artifacts"
          | "browser"
          | "computer"
          | "context"
          | "files"
          | "goals"
          | "mail"
          | "mcp"
          | "messages"
          | "models"
          | "permissions"
          | "schedules"
          | "shell"
          | "skills"
          | "state"
          | "user"
        )[];
    automatic_title?: null | boolean;
    goals_enabled?: null | boolean;
    compaction?: null | {
      model: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      threshold_percent: number;
    };
    report_mode?: null | ("notice" | "inline" | "message");
    model?: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    instructions?: null | {
      project_root: null | string;
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
      standing_instructions: boolean;
      skill_roots: null | string[];
    };
    tools?: {
      [k: string]: {
        timeout_millis: number;
        description: string;
        input_schema: unknown;
        output_schema: unknown;
      };
    } | null;
    children?: {
      [k: string]: {
        id: string;
        revision: string;
      };
    } | null;
    hooks?: {
      [k: string]: {
        operations: null | string[];
        optional: boolean;
        timeout_millis: number;
      };
    } | null;
    output?: null | {
      schema: unknown;
    };
  };
  working_directory: string;
}
export interface CreateTreeResult {
  creation: {
    id: string;
    tree_id: string;
    root_id: string;
    created_at: string;
  };
  tree: null | {
    id: string;
    metadata: {
      title: null | string;
      archived: boolean;
      pinned: boolean;
    };
    engine: "starlark" | "quickjs";
    revision: string;
    created_at: string;
  };
  root: null | {
    history_revision: string;
    id: string;
    tree_id: string;
    parent_id: null | string;
    definition: {
      id: string;
      revision: string;
    };
    config_revision: string;
    configuration: {
      mcp_servers: {
        [k: string]: unknown;
      };
      /**
       * @maxItems 17
       */
      modules: (
        | "agents"
        | "artifacts"
        | "browser"
        | "computer"
        | "context"
        | "files"
        | "goals"
        | "mail"
        | "mcp"
        | "messages"
        | "models"
        | "permissions"
        | "schedules"
        | "shell"
        | "skills"
        | "state"
        | "user"
      )[];
      tools_definition: null | {
        id: string;
        revision: string;
      };
      hooks_definition: null | {
        id: string;
        revision: string;
      };
      automatic_title: boolean;
      goals_enabled: boolean;
      compaction: {
        model: null | {
          provider: string;
          name: string;
          effort: string;
          temperature?: null | number;
          top_p?: null | number;
        };
        threshold_percent: number;
      };
      report_mode: "notice" | "inline" | "message";
      model:
        | {
            provider: string;
            name: string;
            effort: string;
            temperature?: null | number;
            top_p?: null | number;
          }
        | {
            provider: "";
            name: "";
            effort: "";
            temperature?: null | number;
            top_p?: null | number;
          };
      instructions: {
        project_root: null | string;
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
        standing_instructions: boolean;
        skill_roots: null | string[];
      };
      tools: {
        [k: string]: {
          timeout_millis: number;
          description: string;
          input_schema: unknown;
          output_schema: unknown;
        };
      } | null;
      children: {
        [k: string]: {
          id: string;
          revision: string;
        };
      } | null;
      hooks: {
        [k: string]: {
          operations: null | string[];
          optional: boolean;
          timeout_millis: number;
        };
      } | null;
      output_schema: unknown;
    };
    working_directory: string;
    lifecycle: "active" | "stopped";
    created_at: string;
  };
  deleted: boolean;
}
export interface CurrentGoalResult {
  goal: null | {
    id: string;
    revision: string;
    session_id: string;
    spec: {
      text: string;
      max_continuations: string;
    };
    state: "armed" | "paused" | "completed" | "cancelled" | "superseded";
    continuations_used: string;
    stop_reason: null | string;
    origin_formulation_attempt_id: null | string;
    completion_turn_id: null | string;
    completion_operation_id: null | string;
    created_at: string;
  };
}
export interface DefaultPermissionMode {
  mode: "prompt" | "automatic";
  revision: string;
}
export interface Definition {
  ref: {
    id: string;
    revision: string;
  };
  document: {
    id: string;
    name: string;
    defaults: {
      mcp_servers?: {
        [k: string]: unknown;
      } & (null | {
        all: boolean;
        /**
         * @maxItems 64
         */
        servers: string[];
      });
      /**
       * @maxItems 17
       */
      modules?:
        | null
        | (
            | "agents"
            | "artifacts"
            | "browser"
            | "computer"
            | "context"
            | "files"
            | "goals"
            | "mail"
            | "mcp"
            | "messages"
            | "models"
            | "permissions"
            | "schedules"
            | "shell"
            | "skills"
            | "state"
            | "user"
          )[];
      automatic_title?: null | boolean;
      goals_enabled?: null | boolean;
      compaction?: null | {
        model: null | {
          provider: string;
          name: string;
          effort: string;
          temperature?: null | number;
          top_p?: null | number;
        };
        threshold_percent: number;
      };
      report_mode?: null | ("notice" | "inline" | "message");
      model?: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      instructions?: null | {
        project_root: null | string;
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
        standing_instructions: boolean;
        skill_roots: null | string[];
      };
      tools?: {
        [k: string]: {
          timeout_millis: number;
          description: string;
          input_schema: unknown;
          output_schema: unknown;
        };
      } | null;
      children?: {
        [k: string]: {
          id: string;
          revision: string;
        };
      } | null;
      hooks?: {
        [k: string]: {
          operations: null | string[];
          optional: boolean;
          timeout_millis: number;
        };
      } | null;
      output?: null | {
        schema: unknown;
      };
    };
  };
  created_at: string;
}
export interface DefinitionDocument {
  id: string;
  name: string;
  defaults: {
    mcp_servers?: {
      [k: string]: unknown;
    } & (null | {
      all: boolean;
      /**
       * @maxItems 64
       */
      servers: string[];
    });
    /**
     * @maxItems 17
     */
    modules?:
      | null
      | (
          | "agents"
          | "artifacts"
          | "browser"
          | "computer"
          | "context"
          | "files"
          | "goals"
          | "mail"
          | "mcp"
          | "messages"
          | "models"
          | "permissions"
          | "schedules"
          | "shell"
          | "skills"
          | "state"
          | "user"
        )[];
    automatic_title?: null | boolean;
    goals_enabled?: null | boolean;
    compaction?: null | {
      model: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      threshold_percent: number;
    };
    report_mode?: null | ("notice" | "inline" | "message");
    model?: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    instructions?: null | {
      project_root: null | string;
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
      standing_instructions: boolean;
      skill_roots: null | string[];
    };
    tools?: {
      [k: string]: {
        timeout_millis: number;
        description: string;
        input_schema: unknown;
        output_schema: unknown;
      };
    } | null;
    children?: {
      [k: string]: {
        id: string;
        revision: string;
      };
    } | null;
    hooks?: {
      [k: string]: {
        operations: null | string[];
        optional: boolean;
        timeout_millis: number;
      };
    } | null;
    output?: null | {
      schema: unknown;
    };
  };
}
export interface DefinitionRef {
  id: string;
  revision: string;
}
export interface DeleteResult {
  deleted: boolean;
}
export interface EmptyParams {}
export interface ExecutorAccepted {
  accepted: true;
}
export interface ExecutorActivityResult {
  activity: null | {
    epoch: string;
    turn_id: string;
    revision: string;
    /**
     * @maxItems 8
     */
    decisions: {
      invocation_id: null | string;
      hook: "before_tool" | "before_spawn" | "turn_start";
      operation: string;
      decision: "deny" | "rewrite" | "skipped";
      reason: string;
    }[];
    truncated: boolean;
    progress: null | {
      invocation_id: string;
      operation_id: string;
      text: string;
    };
  };
}
export interface ExecutorBindParams {
  definition: {
    id: string;
    revision: string;
  };
  /**
   * @maxItems 128
   */
  tools: string[];
  /**
   * @maxItems 3
   */
  hooks: [] | [string] | [string, string] | [string, string, string];
}
export type ExecutorEvent = {
  jsonrpc: "2.0";
  method: "executor.invoke" | "executor.cancel";
  epoch: string;
  generation: string;
  invocation_id: string;
  invocation: null | {
    origin: "cell" | "host_operation" | "turn";
    invocation_id: string;
    lease: {
      epoch: string;
      definition: {
        id: string;
        revision: string;
      };
      generation: string;
      /**
       * @maxItems 128
       */
      tools: string[];
      /**
       * @maxItems 3
       */
      hooks: [] | [string] | [string, string] | [string, string, string];
    };
    kind: "tool" | "hook";
    name: string;
    session_id: string;
    turn_id: string;
    cell_id: null | string;
    operation_id: null | string;
    operation: string;
    arguments_base64: null | string;
    spawn_base64: null | string;
    input_preview: string;
    permission_mode: string;
    deadline_millis: string;
  };
} & (
  | {
      invocation?: {
        [k: string]: unknown;
      };
      method?: "executor.invoke";
      [k: string]: unknown;
    }
  | {
      invocation?: null;
      method?: "executor.cancel";
      [k: string]: unknown;
    }
);
export interface ExecutorHookResultParams {
  epoch: string;
  generation: string;
  invocation_id: string;
  decision: "" | "allow" | "deny";
  reason: string;
  arguments_base64: null | string;
  spawn_base64: null | string;
  context: string;
  failure: string;
}
export interface ExecutorLease {
  epoch: string;
  definition: {
    id: string;
    revision: string;
  };
  generation: string;
  /**
   * @maxItems 128
   */
  tools: string[];
  /**
   * @maxItems 3
   */
  hooks: [] | [string] | [string, string] | [string, string, string];
}
export interface ExecutorPendingParams {
  epoch: string;
  definition: {
    id: string;
    revision: string;
  };
  generation: string;
  after: null | string;
}
export interface ExecutorPendingResult {
  /**
   * @maxItems 4
   */
  items:
    | []
    | [
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        }
      ]
    | [
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        }
      ]
    | [
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        }
      ]
    | [
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        },
        {
          origin: "cell" | "host_operation" | "turn";
          invocation_id: string;
          lease: {
            epoch: string;
            definition: {
              id: string;
              revision: string;
            };
            generation: string;
            /**
             * @maxItems 128
             */
            tools: string[];
            /**
             * @maxItems 3
             */
            hooks: [] | [string] | [string, string] | [string, string, string];
          };
          kind: "tool" | "hook";
          name: string;
          session_id: string;
          turn_id: string;
          cell_id: null | string;
          operation_id: null | string;
          operation: string;
          arguments_base64: null | string;
          spawn_base64: null | string;
          input_preview: string;
          permission_mode: string;
          deadline_millis: string;
        }
      ];
  next_after: null | string;
}
export interface ExecutorProgressParams {
  epoch: string;
  generation: string;
  invocation_id: string;
  text: string;
}
export interface ExecutorToolResultParams {
  epoch: string;
  generation: string;
  invocation_id: string;
  output_base64: null | string;
  failure: string;
}
export interface ForkParams {
  fork_id: string;
  session_id: string;
  expected_history_revision: string;
  expected_config_revision: string;
  observed_through: string;
  keep_through: string;
  title: null | string;
}
export interface ForkResult {
  fork: {
    id: string;
    session_id: string;
    expected_history_revision: string;
    expected_config_revision: string;
    observed_through: string;
    keep_through: string;
    title: null | string;
    tree_id: string;
    root_id: string;
    created_at: string;
  };
  tree: null | {
    id: string;
    metadata: {
      title: null | string;
      archived: boolean;
      pinned: boolean;
    };
    engine: "starlark" | "quickjs";
    revision: string;
    created_at: string;
  };
  root: null | {
    history_revision: string;
    id: string;
    tree_id: string;
    parent_id: null | string;
    definition: {
      id: string;
      revision: string;
    };
    config_revision: string;
    configuration: {
      mcp_servers: {
        [k: string]: unknown;
      };
      /**
       * @maxItems 17
       */
      modules: (
        | "agents"
        | "artifacts"
        | "browser"
        | "computer"
        | "context"
        | "files"
        | "goals"
        | "mail"
        | "mcp"
        | "messages"
        | "models"
        | "permissions"
        | "schedules"
        | "shell"
        | "skills"
        | "state"
        | "user"
      )[];
      tools_definition: null | {
        id: string;
        revision: string;
      };
      hooks_definition: null | {
        id: string;
        revision: string;
      };
      automatic_title: boolean;
      goals_enabled: boolean;
      compaction: {
        model: null | {
          provider: string;
          name: string;
          effort: string;
          temperature?: null | number;
          top_p?: null | number;
        };
        threshold_percent: number;
      };
      report_mode: "notice" | "inline" | "message";
      model:
        | {
            provider: string;
            name: string;
            effort: string;
            temperature?: null | number;
            top_p?: null | number;
          }
        | {
            provider: "";
            name: "";
            effort: "";
            temperature?: null | number;
            top_p?: null | number;
          };
      instructions: {
        project_root: null | string;
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
        standing_instructions: boolean;
        skill_roots: null | string[];
      };
      tools: {
        [k: string]: {
          timeout_millis: number;
          description: string;
          input_schema: unknown;
          output_schema: unknown;
        };
      } | null;
      children: {
        [k: string]: {
          id: string;
          revision: string;
        };
      } | null;
      hooks: {
        [k: string]: {
          operations: null | string[];
          optional: boolean;
          timeout_millis: number;
        };
      } | null;
      output_schema: unknown;
    };
    working_directory: string;
    lifecycle: "active" | "stopped";
    created_at: string;
  };
  deleted: boolean;
}
export interface FormulateGoalParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
  request: {
    goal_id: string;
    expected_current: null | {
      id: string;
      revision: string;
    };
    max_continuations?: null | string;
    start: boolean;
    tail_messages?: 0 | number;
  };
}
export interface GetStateParams {
  session_id: string;
  scope: "session" | "tree";
  key: string;
}
export interface Goal {
  id: string;
  revision: string;
  session_id: string;
  spec: {
    text: string;
    max_continuations: string;
  };
  state: "armed" | "paused" | "completed" | "cancelled" | "superseded";
  continuations_used: string;
  stop_reason: null | string;
  origin_formulation_attempt_id: null | string;
  completion_turn_id: null | string;
  completion_operation_id: null | string;
  created_at: string;
}
export interface GoalAdmission {
  id: string;
  goal: null | {
    id: string;
    revision: string;
    session_id: string;
    spec: {
      text: string;
      max_continuations: string;
    };
    state: "armed" | "paused" | "completed" | "cancelled" | "superseded";
    continuations_used: string;
    stop_reason: null | string;
    origin_formulation_attempt_id: null | string;
    completion_turn_id: null | string;
    completion_operation_id: null | string;
    created_at: string;
  };
  current: boolean;
  initial: null | {
    receipt: {
      identity: {
        client_id: string;
        request_id: string;
      };
      digest: string;
      input_id: null | string;
      deleted_at: null | string;
      created_at: string;
    };
    input:
      | {
          host_operation: null;
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "prompt";
          /**
           * @minItems 1
           * @maxItems 128
           */
          parts: [
            (
              | {
                  text: string;
                  type: "text";
                }
              | {
                  reference_id: string;
                  type: "content";
                }
            ),
            ...(
              | {
                  text: string;
                  type: "text";
                }
              | {
                  reference_id: string;
                  type: "content";
                }
            )[]
          ];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | {
          host_operation: null;
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "compact" | "goal_formulation" | "automatic_title";
          /**
           * @maxItems 0
           */
          parts: [];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | {
          host_operation: {
            module: "shell" | "files" | "tools" | "computer";
            name: string;
            arguments_base64: string;
          };
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user";
          kind: "host_operation";
          /**
           * @maxItems 0
           */
          parts: [];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | null;
    turn: null | {
      history_revision: string;
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
      config_revision: string;
      state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
      failure: null | string;
      started_at: string;
      finished_at: null | string;
    };
  };
  deleted_at: null | string;
}
export interface GoalChange {
  goal: {
    id: string;
    revision: string;
    session_id: string;
    spec: {
      text: string;
      max_continuations: string;
    };
    state: "armed" | "paused" | "completed" | "cancelled" | "superseded";
    continuations_used: string;
    stop_reason: null | string;
    origin_formulation_attempt_id: null | string;
    completion_turn_id: null | string;
    completion_operation_id: null | string;
    created_at: string;
  };
  cancel_turn_id: null | string;
}
export interface GoalFormulation {
  history_revision: string;
  input_id: string;
  session_id: string;
  request: {
    goal_id: string;
    expected_current: null | {
      id: string;
      revision: string;
    };
    max_continuations?: null | string;
    start: boolean;
    tail_messages?: 0 | number;
  };
  after_sequence: string;
  through_sequence: string;
  turn_id: string;
  attempt_id: string;
  text: string;
  accepted: boolean;
  rejection: null | string;
  created_at: string;
}
export interface GoalFormulationParams {
  session_id: string;
  attempt_id: string;
}
export interface GoalParams {
  session_id: string;
  goal_id: string;
}
export interface Grant {
  id: string;
  session_id: string;
  capability: string;
  resource: string;
  operation_id: null | string;
  issuer_id: null | string;
  created_at: string;
  revoked_at: null | string;
}
export interface GrantParams {
  grant_id: string;
}
export interface GrantsParams {
  session_id: string;
  after?: null | string;
  limit: number;
}
export interface GrantsResult {
  items:
    | null
    | {
        id: string;
        session_id: string;
        capability: string;
        resource: string;
        operation_id: null | string;
        issuer_id: null | string;
        created_at: string;
        revoked_at: null | string;
      }[];
}
export interface HistoryEdit {
  id: string;
  session_id: string;
  digest: string;
  expected_revision: string;
  revision: string;
  observed_through: string;
  keep_through: string;
  created_at: string;
}
export interface HistoryMetadataResult {
  revision: string;
  items:
    | null
    | {
        group_id: string;
        opening_input: boolean;
        source: null | {
          session_id: string;
          message_id: string;
          sequence: string;
        };
        retired_by: null | string;
        retired_revision: null | string;
        id: string;
        session_id: string;
        turn_id: null | string;
        input_id: null | string;
        mail: null | {
          id: string;
          revision: string;
          presentation: "digest" | "body";
        };
        sequence: string;
        role: "system" | "user" | "assistant" | "tool";
        parts_bytes: string;
      }[];
  through_sequence: string;
  next_after: null | string;
}
export interface HistoryPageParams {
  session_id: string;
  direction: "forward" | "backward";
  cursor?: null | string;
  expected_revision?: null | string;
  limit: number;
}
export interface HistoryPageResult {
  snapshot: {
    revision: string;
    session_id: string;
    through_sequence: string;
    message_count: string;
  };
  messages:
    | null
    | (
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "user";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "system";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "assistant";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "tool";
            /**
             * @minItems 1
             * @maxItems 9
             */
            parts: [
              {
                result: {
                  call_id: string;
                  is_error: boolean;
                  output: string;
                };
                type: "tool_result";
              },
              ...{
                reference_id: string;
                type: "content";
              }[]
            ];
            created_at: string;
          }
      )[];
  next_cursor: null | string;
}
export interface HistoryParams {
  expected_revision?: null | string;
  session_id: string;
  after: string;
  limit: number;
}
export interface HistoryResult {
  snapshot: {
    revision: string;
    session_id: string;
    through_sequence: string;
    message_count: string;
  };
  items:
    | null
    | (
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "user";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "system";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "assistant";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "tool";
            /**
             * @minItems 1
             * @maxItems 9
             */
            parts: [
              {
                result: {
                  call_id: string;
                  is_error: boolean;
                  output: string;
                };
                type: "tool_result";
              },
              ...{
                reference_id: string;
                type: "content";
              }[]
            ];
            created_at: string;
          }
      )[];
}
export interface HistorySnapshot {
  revision: string;
  session_id: string;
  through_sequence: string;
  message_count: string;
}
export interface HostAttentionParams {
  after: null | {
    tree_id: string;
    session_id: string;
  };
  limit: number;
  max_bytes: number;
}
export interface HostAttentionResult {
  /**
   * @maxItems 100
   */
  items: {
    tree_id: string;
    root_id: string;
    session_id: string;
    title: null | string;
    activity: {
      session_id: string;
      lifecycle: "active" | "stopped";
      active_turn: null | {
        history_revision: string;
        goal: null | {
          id: string;
          revision: string;
        };
        id: string;
        session_id: string;
        kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
        config_revision: string;
        state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
        failure: null | string;
        started_at: string;
        finished_at: null | string;
      };
      active_input_id: null | string;
      queued_input_count: string;
      pending_permission_count: string;
      pending_question_count: string;
      execution_permit: boolean;
      active_workspace_action_id: null | string;
    };
  }[];
  next_cursor: null | {
    tree_id: string;
    session_id: string;
  };
}
export interface HostDirectoriesParams {
  path: string;
  after: string;
  prefix: string;
  show_hidden: boolean;
  limit: number;
}
export interface HostDirectoriesResult {
  path: string;
  parent: string;
  /**
   * @maxItems 128
   */
  entries: {
    name: string;
    path: string;
  }[];
  next_after: null | string;
  has_more: boolean;
  truncated: boolean;
}
export interface HostDirectoryPickParams {
  start: string;
}
export interface HostDirectoryPickResult {
  path: null | string;
  cancelled: boolean;
}
export type HostOperation = {
  permission_revision: null | string;
  id: string;
  session_id: string;
  turn_id: string;
  cell_id: null | string;
  origin: "cell" | "host_operation";
  request_id: string;
  capability: string;
  resource: string;
  arguments: unknown;
  state: "waiting" | "ready" | "dispatched" | "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
  grant_id: null | string;
  result: null | {
    /**
     * @maxItems 8
     */
    content_references: string[];
    state: "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
    value?: unknown;
    failure?: null | string;
  };
  created_at: string;
  dispatched_at: null | string;
  finished_at: null | string;
} & (
  | {
      cell_id?: {
        [k: string]: unknown;
      };
      origin?: "cell";
      [k: string]: unknown;
    }
  | {
      cell_id?: null;
      origin?: "host_operation";
      [k: string]: unknown;
    }
);
export interface HostOperationParams {
  operation_id: string;
}
export interface HostOperationsParams {
  turn_id: string;
  after?: null | string;
  limit: number;
}
export interface HostOperationsResult {
  items:
    | null
    | (
        | {
            cell_id?: {
              [k: string]: unknown;
            };
            origin?: "cell";
            [k: string]: unknown;
          }
        | {
            cell_id?: null;
            origin?: "host_operation";
            [k: string]: unknown;
          }
      )[];
}
export interface HostSkillsParams {
  scope: "global" | "project";
  cwd: string;
  prefix: string;
  definition: null | {
    id: string;
    revision: string;
  };
  limit: number;
}
export interface HostSkillsResult {
  /**
   * @maxItems 1024
   */
  candidates: {
    text: string;
    description: string;
  }[];
  truncated: boolean;
}
export interface HostThemeResolveParams {
  name: string;
  json: string;
}
export interface HostThemeResolved {
  id: string;
  name: string;
  dark: boolean;
  colors: {
    background: string;
    foreground: string;
    muted: string;
    faint: string;
    primary: string;
    on_primary: string;
    accent: string;
    success: string;
    warning: string;
    error: string;
    info: string;
    link: string;
    emphasis: string;
    border: string;
    border_focus: string;
    diff_add: string;
    diff_del: string;
    panel: string;
    element: string;
    hover: string;
  };
  syntax: {
    keyword: string;
    string: string;
    number: string;
    comment: string;
    function: string;
    type: string;
    operator: string;
    punctuation: string;
  };
  markdown: {
    heading: string;
    strong: string;
    code: string;
    quote: string;
  };
  code: {
    foreground: string;
    background: string;
    tokens: {
      [k: string]: {
        color: string;
        background: string;
        bold: boolean;
        italic: boolean;
        underline: boolean;
      };
    } | null;
  };
  web?: null | {
    navigation?: string;
    quiet_border?: string;
    code_background?: string;
    inline_code_background?: string;
  };
}
export interface HostThemesResult {
  /**
   * @maxItems 256
   */
  themes: {
    id: string;
    name: string;
    dark: boolean;
    source: "builtin" | "custom";
  }[];
  /**
   * @maxItems 128
   */
  errors: {
    file: string;
    message: string;
  }[];
  truncated: boolean;
}
export interface HostToolSchemasResult {
  /**
   * @maxItems 136
   */
  items: {
    module: "shell" | "files" | "tools" | "computer";
    name: string;
    description: string;
    input_schema: unknown;
  }[];
}
export interface InferenceAccountStatus {
  management_state: "absent" | "stored" | "expired" | "unavailable";
  inference_state: "absent" | "stored" | "unavailable";
  route_state: "missing" | "configured" | "conflict" | "unavailable";
  user_id: null | string;
  email: null | string;
  expires_at: null | string;
  team_id: null | string;
  team_name: null | string;
  project_id: null | string;
  project_name: null | string;
  failure: null | string;
  cleanup_pending: boolean;
}
export interface InferenceCleanupResult {
  /**
   * @maxItems 64
   */
  items: {
    id: string;
    expires_at: string;
    team_id: null | string;
    key_id: null | string;
    key_state: "absent" | "pending" | "archived";
    session_state: "absent" | "pending" | "retained" | "signed_out";
    failure: null | string;
  }[];
  failure: null | string;
}
export interface InferenceCreateProjectParams {
  flow_id: string;
  name: string;
}
export interface InferenceFlow {
  id: string;
  kind: null | string;
  state:
    | "authorizing"
    | "choose_team"
    | "loading_projects"
    | "choose_project"
    | "creating_project"
    | "provisioning"
    | "persistence_required"
    | "setup_required"
    | "cleanup_required"
    | "succeeded"
    | "failed"
    | "uncertain"
    | "cancelled"
    | "expired"
    | "interrupted";
  verification_url: null | string;
  user_code: null | string;
  expires_at: null | string;
  /**
   * @maxItems 256
   */
  teams: {
    id: string;
    name: string;
    slug: string;
  }[];
  /**
   * @maxItems 256
   */
  projects: {
    id: string;
    name: string;
  }[];
  team_id: null | string;
  project_id: null | string;
  failure: null | string;
}
export interface InferenceFlowParams {
  flow_id: string;
}
export interface InferenceFlowsResult {
  /**
   * @maxItems 64
   */
  items: {
    id: string;
    kind: null | string;
    state:
      | "authorizing"
      | "choose_team"
      | "loading_projects"
      | "choose_project"
      | "creating_project"
      | "provisioning"
      | "persistence_required"
      | "setup_required"
      | "cleanup_required"
      | "succeeded"
      | "failed"
      | "uncertain"
      | "cancelled"
      | "expired"
      | "interrupted";
    verification_url: null | string;
    user_code: null | string;
    expires_at: null | string;
    /**
     * @maxItems 256
     */
    teams: {
      id: string;
      name: string;
      slug: string;
    }[];
    /**
     * @maxItems 256
     */
    projects: {
      id: string;
      name: string;
    }[];
    team_id: null | string;
    project_id: null | string;
    failure: null | string;
  }[];
}
export interface InferenceLogoutResult {
  status: {
    management_state: "absent" | "stored" | "expired" | "unavailable";
    inference_state: "absent" | "stored" | "unavailable";
    route_state: "missing" | "configured" | "conflict" | "unavailable";
    user_id: null | string;
    email: null | string;
    expires_at: null | string;
    team_id: null | string;
    team_name: null | string;
    project_id: null | string;
    project_name: null | string;
    failure: null | string;
    cleanup_pending: boolean;
  };
  local_failure: null | string;
  cleanup_failure: null | string;
  /**
   * @maxItems 64
   */
  cleanup: {
    id: string;
    expires_at: string;
    team_id: null | string;
    key_id: null | string;
    key_state: "absent" | "pending" | "archived";
    session_state: "absent" | "pending" | "retained" | "signed_out";
    failure: null | string;
  }[];
}
export interface InferenceProjectParams {
  flow_id: string;
  project_id: string;
}
export interface InferenceTeamParams {
  flow_id: string;
  team_id: string;
}
export interface InitializeParams {
  network_client?: boolean;
  expected_process_epoch?: null | string;
  major: number;
  expected_runtime_id?: null | string;
}
export interface InitializeResult {
  network_client: boolean;
  process_epoch: string;
  major: number;
  minor: number;
  runtime_id: string;
  builtins:
    | null
    | {
        id: string;
        revision: string;
      }[];
}
export type Input =
  | {
      host_operation: null;
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      source: "user" | "agent" | "schedule" | "goal";
      kind: "prompt";
      /**
       * @minItems 1
       * @maxItems 128
       */
      parts: [
        (
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        ),
        ...(
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        )[]
      ];
      state: "queued" | "claimed" | "cancelled";
      turn_id: null | string;
      created_at: string;
      schedule: null | {
        schedule_id: string;
        scheduled_for: string;
      };
    }
  | {
      host_operation: null;
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      source: "user" | "agent" | "schedule" | "goal";
      kind: "compact" | "goal_formulation" | "automatic_title";
      /**
       * @maxItems 0
       */
      parts: [];
      state: "queued" | "claimed" | "cancelled";
      turn_id: null | string;
      created_at: string;
      schedule: null | {
        schedule_id: string;
        scheduled_for: string;
      };
    }
  | {
      host_operation: {
        module: "shell" | "files" | "tools" | "computer";
        name: string;
        arguments_base64: string;
      };
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      source: "user";
      kind: "host_operation";
      /**
       * @maxItems 0
       */
      parts: [];
      state: "queued" | "claimed" | "cancelled";
      turn_id: null | string;
      created_at: string;
      schedule: null | {
        schedule_id: string;
        scheduled_for: string;
      };
    };
export interface InputPageParams {
  session_id: string;
  state: "queued" | "all";
  after?: null | string;
  limit: number;
}
export interface InputPageResult {
  items:
    | null
    | {
        id: string;
        session_id: string;
        ordinal: string;
        source: "user" | "agent" | "schedule" | "goal";
        kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
        state: "queued" | "claimed" | "cancelled";
        turn_id: null | string;
        created_at: string;
        text_preview: string;
        preview_truncated: boolean;
        attachment_count: string;
      }[];
  next_cursor: null | string;
}
export interface InputParams {
  input_id: string;
}
export interface InstructionManifestResult {
  manifest: null | {
    bytes: string;
    sha256: string;
    /**
     * @maxItems 1152
     */
    sources: {
      kind: "project_file" | "skill_metadata" | "invoked_skill" | "standing_instructions";
      scope: "workspace" | "host" | "project";
      root_id: null | string;
      path: string;
      bytes: string;
      sha256: string;
    }[];
  };
}
export interface LanguageServersResult {
  /**
   * @maxItems 16
   */
  items: {
    name: string;
    state: "connected" | "not_started" | "failed";
    workspace_root: null | string;
    failure: null | string;
  }[];
}
export interface LifecycleParams {
  session_id: string;
  lifecycle: "active" | "stopped";
}
export interface ListCompletionsParams {
  parent_id: string;
  after?: null | string;
  limit: number;
}
export interface ListCompletionsResult {
  items:
    | null
    | {
        parent_id: string;
        child_id: string;
        turn_id: string;
        input_id: null | string;
        message_id: null | string;
        state: "succeeded" | "failed" | "cancelled" | "interrupted";
        failure: null | string;
        mode: "notice" | "inline" | "message";
        finished_at: string;
        text_bytes: string;
        omitted_parts: string;
      }[];
}
export interface ListDefinitionsParams {
  after?: null | {
    id: string;
    revision: string;
  };
  limit: number;
}
export interface ListDefinitionsResult {
  /**
   * @maxItems 100
   */
  items: {
    ref: {
      id: string;
      revision: string;
    };
    name: string;
    created_at: string;
  }[];
  next_cursor: null | {
    id: string;
    revision: string;
  };
}
export interface ListMailParams {
  session_id: string;
  state?: null | ("pending" | "delivered" | "done");
  after?: null | string;
  limit: number;
}
export interface ListMailResult {
  items:
    | null
    | {
        id: string;
        revision: string;
        source: {
          kind: "session" | "state" | "completion";
          id: string;
        };
        recipient_id: string;
        delivery: "queued" | "steer" | "next_turn";
        subject: string;
        body_bytes: string;
        evidence_ref: null | string;
        state: "pending" | "delivered" | "done";
        available_at: string;
        created_at: string;
        revised_at: string;
      }[];
}
export interface ListSchedulesParams {
  session_id: string;
  after?: string;
  upcoming?: boolean;
  cursor?: null | {
    due: string;
    id: string;
  };
  limit: number;
}
export interface ListSessionsParams {
  tree_id: string;
  after?: null | string;
  limit: number;
}
export interface ListSessionsResult {
  items:
    | null
    | {
        history_revision: string;
        id: string;
        tree_id: string;
        parent_id: null | string;
        definition: {
          id: string;
          revision: string;
        };
        config_revision: string;
        configuration: {
          mcp_servers: {
            [k: string]: unknown;
          };
          /**
           * @maxItems 17
           */
          modules: (
            | "agents"
            | "artifacts"
            | "browser"
            | "computer"
            | "context"
            | "files"
            | "goals"
            | "mail"
            | "mcp"
            | "messages"
            | "models"
            | "permissions"
            | "schedules"
            | "shell"
            | "skills"
            | "state"
            | "user"
          )[];
          tools_definition: null | {
            id: string;
            revision: string;
          };
          hooks_definition: null | {
            id: string;
            revision: string;
          };
          automatic_title: boolean;
          goals_enabled: boolean;
          compaction: {
            model: null | {
              provider: string;
              name: string;
              effort: string;
              temperature?: null | number;
              top_p?: null | number;
            };
            threshold_percent: number;
          };
          report_mode: "notice" | "inline" | "message";
          model:
            | {
                provider: string;
                name: string;
                effort: string;
                temperature?: null | number;
                top_p?: null | number;
              }
            | {
                provider: "";
                name: "";
                effort: "";
                temperature?: null | number;
                top_p?: null | number;
              };
          instructions: {
            project_root: null | string;
            text: string;
            project_files: null | string[];
            discover_skills: boolean;
            standing_instructions: boolean;
            skill_roots: null | string[];
          };
          tools: {
            [k: string]: {
              timeout_millis: number;
              description: string;
              input_schema: unknown;
              output_schema: unknown;
            };
          } | null;
          children: {
            [k: string]: {
              id: string;
              revision: string;
            };
          } | null;
          hooks: {
            [k: string]: {
              operations: null | string[];
              optional: boolean;
              timeout_millis: number;
            };
          } | null;
          output_schema: unknown;
        };
        working_directory: string;
        lifecycle: "active" | "stopped";
        created_at: string;
      }[];
}
export interface ListSkillsParams {
  session_id: string;
  prefix?: string;
  after?: string;
  limit: number;
}
export interface ListSkillsResult {
  /**
   * @maxItems 100
   */
  items: {
    name: string;
    description: string;
    disabled: boolean;
    source: {
      kind: "project_file" | "skill_metadata" | "invoked_skill" | "standing_instructions";
      scope: "workspace" | "host" | "project";
      root_id: null | string;
      path: string;
      bytes: string;
      sha256: string;
    };
  }[];
  next_after: null | string;
}
export interface ListStateParams {
  session_id: string;
  scope: "session" | "tree";
  after?: null | string;
  limit: number;
}
export interface ListTreesParams {
  expected_revision?: null | string;
  after?: null | string;
  archived?: null | boolean;
  pinned?: null | boolean;
  limit: number;
}
export interface ListTreesResult {
  revision: string;
  /**
   * @maxItems 100
   */
  items: {
    tree: {
      id: string;
      metadata: {
        title: null | string;
        archived: boolean;
        pinned: boolean;
      };
      engine: "starlark" | "quickjs";
      revision: string;
      created_at: string;
    };
    root_id: string;
  }[];
  next_cursor: null | string;
}
export interface MCPAttachParams {
  session_id: string;
  servers: {
    [k: string]: {
      /**
       * @maxItems 128
       */
      command: string[];
      env: {
        [k: string]: string;
      };
      cwd: string;
      url: string;
      headers: {
        [k: string]: string;
      };
      enabled: null | boolean;
      note: string;
      startup_timeout_seconds: number;
      tool_timeout_seconds: number;
    };
  };
}
export interface MCPBrandIconsParams {
  /**
   * @maxItems 64
   */
  keys: string[];
}
export interface MCPBrandIconsResult {
  icons: {
    [k: string]: string;
  };
}
export interface MCPConfiguration {
  revision: string;
  /**
   * @maxItems 64
   */
  servers: {
    name: string;
    transport: "stdio" | "http";
    enabled: boolean;
    startup_timeout_seconds: number;
    tool_timeout_seconds: number;
    brand_hint: string;
    brand_key: string;
  }[];
  imports: {
    claude: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    codex: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    project: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    opencode: null | {
      enabled: null | boolean;
      /**
       * @maxItems 256
       */
      only: string[];
      /**
       * @maxItems 256
       */
      exclude: string[];
    };
    offered: boolean;
  };
  brand_icons: boolean;
}
export interface MCPImportCandidatesParams {
  session_id: null | string;
}
export interface MCPImportCandidatesResult {
  revision: string;
  /**
   * @maxItems 256
   */
  candidates: {
    fingerprint: string;
    name: string;
    source: "codex" | "claude" | "project" | "opencode";
    state: "importable" | "native" | "disabled" | "excluded" | "unsupported";
    gated: boolean;
    note: string;
    brand_hint: string;
    brand_key: string;
  }[];
  source_errors: {
    [k: string]: string;
  };
}
export interface MCPImportParams {
  session_id: null | string;
  revision: string;
  fingerprints: {
    [k: string]: string;
  };
}
export interface MCPImportResult {
  configuration: {
    revision: string;
    /**
     * @maxItems 64
     */
    servers: {
      name: string;
      transport: "stdio" | "http";
      enabled: boolean;
      startup_timeout_seconds: number;
      tool_timeout_seconds: number;
      brand_hint: string;
      brand_key: string;
    }[];
    imports: {
      claude: null | {
        enabled: null | boolean;
        /**
         * @maxItems 256
         */
        only: string[];
        /**
         * @maxItems 256
         */
        exclude: string[];
      };
      codex: null | {
        enabled: null | boolean;
        /**
         * @maxItems 256
         */
        only: string[];
        /**
         * @maxItems 256
         */
        exclude: string[];
      };
      project: null | {
        enabled: null | boolean;
        /**
         * @maxItems 256
         */
        only: string[];
        /**
         * @maxItems 256
         */
        exclude: string[];
      };
      opencode: null | {
        enabled: null | boolean;
        /**
         * @maxItems 256
         */
        only: string[];
        /**
         * @maxItems 256
         */
        exclude: string[];
      };
      offered: boolean;
    };
    brand_icons: boolean;
  };
  /**
   * @maxItems 64
   */
  added: string[];
  skipped: {
    [k: string]: string;
  };
}
export interface MCPInstructionsResult {
  server: string;
  generation: string;
  resource: string;
  text: string;
  /**
   * @maxItems 4
   */
  content_parts:
    | []
    | [
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        }
      ]
    | [
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        }
      ]
    | [
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        }
      ]
    | [
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        },
        {
          id: string;
          session_id: string;
          digest: string;
          size: string;
          media_type: string;
          created_at: string;
        }
      ];
  bytes: string;
}
export interface MCPRefreshResult {
  /**
   * @maxItems 64
   */
  added: string[];
  /**
   * @maxItems 64
   */
  existing: string[];
  /**
   * @maxItems 64
   */
  changed: string[];
  /**
   * @maxItems 64
   */
  servers: {
    name: string;
    state: "not_started" | "disabled" | "connecting" | "ready" | "failed" | "blocked" | "unreadable";
    note: string;
    failure: null | string;
    tools: number;
    source: string;
  }[];
  /**
   * @maxItems 256
   */
  blocked: {
    name: string;
    state: "not_started" | "disabled" | "connecting" | "ready" | "failed" | "blocked" | "unreadable";
    note: string;
    failure: null | string;
    tools: number;
    source: string;
  }[];
  /**
   * @maxItems 16
   */
  source_errors: {
    name: string;
    state: "not_started" | "disabled" | "connecting" | "ready" | "failed" | "blocked" | "unreadable";
    note: string;
    failure: null | string;
    tools: number;
    source: string;
  }[];
}
export interface MCPServerParams {
  session_id: string;
  server: string;
}
export interface MCPStatusResult {
  /**
   * @maxItems 336
   */
  items: {
    name: string;
    state: "not_started" | "disabled" | "connecting" | "ready" | "failed" | "blocked" | "unreadable";
    note: string;
    failure: null | string;
    tools: number;
    source: string;
  }[];
}
export interface MCPToolsResult {
  /**
   * @maxItems 2048
   */
  items: {
    name: string;
    title: string;
    description: string;
    input_schema: unknown;
    server: string;
    generation: string;
    capability: "mcp.call" | "mcp.call.trusted";
    resource: string;
  }[];
}
export interface MailAdmission {
  mail_id: string;
  mail: null | {
    id: string;
    revision: string;
    source: {
      kind: "session" | "state" | "completion";
      id: string;
    };
    recipient_id: string;
    delivery: "queued" | "steer" | "next_turn";
    subject: string;
    body_bytes: string;
    evidence_ref: null | string;
    state: "pending" | "delivered" | "done";
    available_at: string;
    created_at: string;
    revised_at: string;
  };
  deleted_at: null | string;
}
export interface MatchReceiptParams {
  method:
    | "sessions.submit"
    | "sessions.compact"
    | "sessions.spawn"
    | "goals.formulate"
    | "goals.resume"
    | "tool.call"
    | "shell.run";
  params_base64: string;
}
export type Message =
  | {
      group_id: string;
      opening_input: boolean;
      source: null | {
        session_id: string;
        message_id: string;
        sequence: string;
      };
      retired_by: null | string;
      retired_revision: null | string;
      id: string;
      session_id: string;
      turn_id: null | string;
      input_id: null | string;
      mail: null | {
        id: string;
        revision: string;
        presentation: "digest" | "body";
      };
      sequence: string;
      role: "user";
      /**
       * @minItems 1
       * @maxItems 128
       */
      parts: [
        (
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        ),
        ...(
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        )[]
      ];
      created_at: string;
    }
  | {
      group_id: string;
      opening_input: boolean;
      source: null | {
        session_id: string;
        message_id: string;
        sequence: string;
      };
      retired_by: null | string;
      retired_revision: null | string;
      id: string;
      session_id: string;
      turn_id: null | string;
      input_id: null | string;
      mail: null | {
        id: string;
        revision: string;
        presentation: "digest" | "body";
      };
      sequence: string;
      role: "system";
      /**
       * @minItems 1
       * @maxItems 128
       */
      parts: [
        (
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        ),
        ...(
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
        )[]
      ];
      created_at: string;
    }
  | {
      group_id: string;
      opening_input: boolean;
      source: null | {
        session_id: string;
        message_id: string;
        sequence: string;
      };
      retired_by: null | string;
      retired_revision: null | string;
      id: string;
      session_id: string;
      turn_id: null | string;
      input_id: null | string;
      mail: null | {
        id: string;
        revision: string;
        presentation: "digest" | "body";
      };
      sequence: string;
      role: "assistant";
      /**
       * @minItems 1
       * @maxItems 128
       */
      parts: [
        (
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
          | {
              call: {
                arguments: {
                  [k: string]: unknown;
                };
                id: string;
                name: string;
              };
              type: "tool_call";
            }
        ),
        ...(
          | {
              text: string;
              type: "text";
            }
          | {
              reference_id: string;
              type: "content";
            }
          | {
              call: {
                arguments: {
                  [k: string]: unknown;
                };
                id: string;
                name: string;
              };
              type: "tool_call";
            }
        )[]
      ];
      created_at: string;
    }
  | {
      group_id: string;
      opening_input: boolean;
      source: null | {
        session_id: string;
        message_id: string;
        sequence: string;
      };
      retired_by: null | string;
      retired_revision: null | string;
      id: string;
      session_id: string;
      turn_id: null | string;
      input_id: null | string;
      mail: null | {
        id: string;
        revision: string;
        presentation: "digest" | "body";
      };
      sequence: string;
      role: "tool";
      /**
       * @minItems 1
       * @maxItems 9
       */
      parts: [
        {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        },
        ...{
          reference_id: string;
          type: "content";
        }[]
      ];
      created_at: string;
    };
export interface ModelAttemptsParams {
  turn_id: string;
  after?: null | string;
  limit: number;
}
export interface ModelAttemptsResult {
  items:
    | null
    | {
        id: string;
        turn_id: string;
        logical_id: string;
        number: number;
        operation_id: null | string;
        batch_index: null | number;
        request: {
          purpose: "turn" | "compaction" | "goal_formulation" | "model_helper" | "automatic_title";
          model: {
            provider: string;
            name: string;
            effort: string;
            temperature?: null | number;
            top_p?: null | number;
          };
          route: string;
          adapter: string;
          request_digest: string;
          prices: {
            input: null | string;
            output: null | string;
            reasoning: null | string;
            cached_input: null | string;
            cached_output: null | string;
          };
          max_output_tokens: string;
          input_token_bound: null | string;
          timeout_millis: string;
        };
        state: "reserved" | "dispatched" | "succeeded" | "failed" | "cancelled" | "uncertain";
        result: null | {
          state: "succeeded" | "failed" | "cancelled" | "uncertain";
          usage: {
            input: null | string;
            output: null | string;
            reasoning: null | string;
            cached_input: null | string;
            cached_output: null | string;
          };
          reported_cost_nano_usd: null | string;
          failure: null | string;
          usage_note: null | string;
          elapsed_millis: null | string;
        };
        cost_nano_usd: null | string;
        cost_source: "unknown" | "provider" | "prices" | "not_dispatched";
        cost_note: null | string;
        message_id: null | string;
        created_at: string;
        dispatched_at: null | string;
        finished_at: null | string;
      }[];
}
export interface OpenAIAccountStatus {
  auth_state: "signed_out" | "stored" | "sign_in_required" | "unavailable";
  route_state: "configured" | "missing" | "conflict" | "unavailable";
  account_id: null | string;
  email: null | string;
  plan: null | string;
  expires_at: null | string;
  failure: null | string;
}
export interface OpenAIFlowParams {
  flow_id: string;
}
export interface OpenAIFlowsResult {
  /**
   * @maxItems 64
   */
  items: {
    id: string;
    state: "authorizing" | "succeeded" | "setup_required" | "failed" | "cancelled" | "expired" | "interrupted";
    verification_url: null | string;
    user_code: null | string;
    expires_at: null | string;
    failure: null | string;
  }[];
}
export interface OpenAILoginFlow {
  id: string;
  state: "authorizing" | "succeeded" | "setup_required" | "failed" | "cancelled" | "expired" | "interrupted";
  verification_url: null | string;
  user_code: null | string;
  expires_at: null | string;
  failure: null | string;
}
export type Part =
  | {
      text: string;
      type: "text";
    }
  | {
      reference_id: string;
      type: "content";
    }
  | {
      call: {
        arguments: {
          [k: string]: unknown;
        };
        id: string;
        name: string;
      };
      type: "tool_call";
    }
  | {
      result: {
        call_id: string;
        is_error: boolean;
        output: string;
      };
      type: "tool_result";
    };
export interface Permission {
  operation_id: string;
  state: "pending" | "approved" | "denied" | "cancelled";
  created_at: string;
  resolved_at: null | string;
}
export interface PermissionModeEdit {
  id: string;
  session_id: string;
  expected_revision: string;
  mode: "prompt" | "automatic";
  previous_mode: "prompt" | "automatic";
  policy: {
    tree_id: string;
    mode: "prompt" | "automatic";
    revision: string;
    updated_at: string;
  };
  created_at: string;
}
export interface PermissionModeEditParams {
  session_id: string;
  edit_id: string;
}
export interface PermissionPolicy {
  tree_id: string;
  mode: "prompt" | "automatic";
  revision: string;
  updated_at: string;
}
export interface PermissionsParams {
  session_id: string;
  after?: null | string;
  limit: number;
}
export interface PermissionsResult {
  items:
    | null
    | {
        operation_id: string;
        state: "pending" | "approved" | "denied" | "cancelled";
        created_at: string;
        resolved_at: null | string;
      }[];
}
export interface ProviderCatalog {
  provider: string;
  state: "missing" | "scope_changed" | "cached";
  scope_state: "unverified" | "current";
  discovery:
    "not_checked" | "failed" | "catalog_response" | "account_catalog" | "authenticated_catalog" | "public_catalog";
  fetched_at: null | string;
  stale: boolean;
  failure: null | string;
  /**
   * @maxItems 1024
   */
  models: {
    id: string;
    name: string;
    prices: {
      input: null | string;
      output: null | string;
      reasoning: null | string;
      cached_input: null | string;
      cached_output: null | string;
    };
    context_window_tokens: null | string;
    advertised_context_tokens: null | string;
    effective_context_percent: null | string;
    max_output_tokens: null | string;
    /**
     * @maxItems 32
     */
    reasoning_efforts: null | string[];
    /**
     * @maxItems 32
     */
    input_modalities: null | string[];
    /**
     * @maxItems 32
     */
    output_modalities: null | string[];
    supports_tools: null | boolean;
    metadata_source: "advertised" | "bundled" | "advertised+bundled";
  }[];
}
export interface ProviderDefaultsParams {
  revision: string;
  defaults: {
    selection: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    settings: null | {
      prices: {
        input: null | string;
        output: null | string;
        reasoning: null | string;
        cached_input: null | string;
        cached_output: null | string;
      };
      context_window_tokens: null | string;
      max_output_tokens: string;
      timeout_millis: string;
      max_attempts: number;
    };
  };
}
export interface ProviderInventory {
  revision: string;
  /**
   * @maxItems 128
   */
  routes: {
    id: string;
    kind: "openai-chat" | "openai-responses" | "openai-codex";
    base_url: string;
    credential: {
      source: "env" | "file" | "command" | "none" | "inference-net" | "openai-codex";
      state: "unavailable" | "unchecked" | "not_required" | "missing" | "available" | "refresh_required";
      environment: string;
      file: string;
    };
    models: {
      [k: string]: {
        prices: {
          input: null | string;
          output: null | string;
          reasoning: null | string;
          cached_input: null | string;
          cached_output: null | string;
        };
        context_window_tokens: null | string;
        max_output_tokens: string;
        timeout_millis: string;
        max_attempts: number;
      };
    } | null;
  }[];
  defaults: null | {
    provider: string;
    name: string;
    effort: string;
    temperature?: null | number;
    top_p?: null | number;
  };
  compaction_model: null | {
    provider: string;
    name: string;
    effort: string;
    temperature?: null | number;
    top_p?: null | number;
  };
}
export interface ProviderModelsResult {
  /**
   * @maxItems 1024
   */
  items: {
    id: string;
    name: string;
    prices: {
      input: null | string;
      output: null | string;
      reasoning: null | string;
      cached_input: null | string;
      cached_output: null | string;
    };
    context_window_tokens: null | string;
    advertised_context_tokens: null | string;
    effective_context_percent: null | string;
    max_output_tokens: null | string;
    /**
     * @maxItems 32
     */
    reasoning_efforts: null | string[];
    /**
     * @maxItems 32
     */
    input_modalities: null | string[];
    /**
     * @maxItems 32
     */
    output_modalities: null | string[];
    supports_tools: null | boolean;
    metadata_source: "advertised" | "bundled" | "advertised+bundled";
  }[];
}
export interface ProviderParams {
  provider: string;
}
export interface ProviderPresetsResult {
  /**
   * @maxItems 11
   */
  items: {
    id: string;
    name: string;
    kind: "openai-chat" | "openai-responses" | "openai-codex";
    base_url: string;
    /**
     * @maxItems 2
     */
    methods: [] | [string] | [string, string];
    /**
     * @maxItems 2
     */
    environments: [] | [string] | [string, string];
    key_url: string;
    /**
     * @maxItems 16
     */
    suggested_models: string[];
    suggested_effort: string;
  }[];
}
export interface ProviderReadiness {
  configured: boolean;
  credential_state: "unavailable" | "unchecked" | "not_required" | "missing" | "available" | "refresh_required";
  catalog_state: "missing" | "scope_changed" | "cached";
  model_state: "unknown" | "configured" | "catalogued";
  inference_state: "not_tested";
}
export interface ProviderReadinessParams {
  selection: {
    provider: string;
    name: string;
    effort: string;
    temperature?: null | number;
    top_p?: null | number;
  };
}
export interface PutContentParams {
  session_id: string;
  reference_id: string;
  media_type: string;
  data_base64: string;
}
export type Question =
  | {
      operation_id: string;
      session_id: string;
      turn_id: string;
      cell_id: string;
      request: {
        /**
         * @minItems 1
         * @maxItems 8
         */
        questions: [
          {
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          },
          ...{
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          }[]
        ];
        batch: boolean;
      };
      state: "pending";
      /**
       * @minItems 0
       * @maxItems 0
       */
      answers: [];
      close_reason: null;
      created_at: string;
      deadline: string;
      closed_at: null;
    }
  | {
      operation_id: string;
      session_id: string;
      turn_id: string;
      cell_id: string;
      request: {
        /**
         * @minItems 1
         * @maxItems 8
         */
        questions: [
          {
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          },
          ...{
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          }[]
        ];
        batch: boolean;
      };
      state: "answered";
      /**
       * @minItems 1
       * @maxItems 8
       */
      answers: [
        {
          /**
           * @minItems 0
           * @maxItems 7
           */
          answer: string[];
          dismissed: boolean;
        },
        ...{
          /**
           * @minItems 0
           * @maxItems 7
           */
          answer: string[];
          dismissed: boolean;
        }[]
      ];
      close_reason: null;
      created_at: string;
      deadline: string;
      closed_at: string;
    }
  | {
      operation_id: string;
      session_id: string;
      turn_id: string;
      cell_id: string;
      request: {
        /**
         * @minItems 1
         * @maxItems 8
         */
        questions: [
          {
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          },
          ...{
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          }[]
        ];
        batch: boolean;
      };
      state: "dismissed";
      /**
       * @minItems 1
       * @maxItems 8
       */
      answers: [
        {
          /**
           * @minItems 0
           * @maxItems 7
           */
          answer: string[];
          dismissed: boolean;
        },
        ...{
          /**
           * @minItems 0
           * @maxItems 7
           */
          answer: string[];
          dismissed: boolean;
        }[]
      ];
      close_reason: null;
      created_at: string;
      deadline: string;
      closed_at: string;
    }
  | {
      operation_id: string;
      session_id: string;
      turn_id: string;
      cell_id: string;
      request: {
        /**
         * @minItems 1
         * @maxItems 8
         */
        questions: [
          {
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          },
          ...{
            question: string;
            /**
             * @minItems 2
             * @maxItems 6
             */
            options:
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ]
              | [
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  },
                  {
                    label: string;
                    description: string;
                    recommended: boolean;
                  }
                ];
            multiple: boolean;
          }[]
        ];
        batch: boolean;
      };
      state: "closed";
      /**
       * @minItems 0
       * @maxItems 0
       */
      answers: [];
      close_reason: "cancelled" | "expired" | "interrupted";
      created_at: string;
      deadline: string;
      closed_at: string;
    };
export interface QuestionParams {
  session_id: string;
  operation_id: string;
}
export interface QuestionsParams {
  session_id: string;
  pending_only?: boolean;
  after?: null | string;
  limit: number;
}
export interface QuestionsResult {
  /**
   * @minItems 0
   * @maxItems 100
   */
  items: (
    | {
        operation_id: string;
        session_id: string;
        turn_id: string;
        cell_id: string;
        request: {
          /**
           * @minItems 1
           * @maxItems 8
           */
          questions: [
            {
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            },
            ...{
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            }[]
          ];
          batch: boolean;
        };
        state: "pending";
        /**
         * @minItems 0
         * @maxItems 0
         */
        answers: [];
        close_reason: null;
        created_at: string;
        deadline: string;
        closed_at: null;
      }
    | {
        operation_id: string;
        session_id: string;
        turn_id: string;
        cell_id: string;
        request: {
          /**
           * @minItems 1
           * @maxItems 8
           */
          questions: [
            {
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            },
            ...{
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            }[]
          ];
          batch: boolean;
        };
        state: "answered";
        /**
         * @minItems 1
         * @maxItems 8
         */
        answers: [
          {
            /**
             * @minItems 0
             * @maxItems 7
             */
            answer: string[];
            dismissed: boolean;
          },
          ...{
            /**
             * @minItems 0
             * @maxItems 7
             */
            answer: string[];
            dismissed: boolean;
          }[]
        ];
        close_reason: null;
        created_at: string;
        deadline: string;
        closed_at: string;
      }
    | {
        operation_id: string;
        session_id: string;
        turn_id: string;
        cell_id: string;
        request: {
          /**
           * @minItems 1
           * @maxItems 8
           */
          questions: [
            {
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            },
            ...{
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            }[]
          ];
          batch: boolean;
        };
        state: "dismissed";
        /**
         * @minItems 1
         * @maxItems 8
         */
        answers: [
          {
            /**
             * @minItems 0
             * @maxItems 7
             */
            answer: string[];
            dismissed: boolean;
          },
          ...{
            /**
             * @minItems 0
             * @maxItems 7
             */
            answer: string[];
            dismissed: boolean;
          }[]
        ];
        close_reason: null;
        created_at: string;
        deadline: string;
        closed_at: string;
      }
    | {
        operation_id: string;
        session_id: string;
        turn_id: string;
        cell_id: string;
        request: {
          /**
           * @minItems 1
           * @maxItems 8
           */
          questions: [
            {
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            },
            ...{
              question: string;
              /**
               * @minItems 2
               * @maxItems 6
               */
              options:
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ]
                | [
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    },
                    {
                      label: string;
                      description: string;
                      recommended: boolean;
                    }
                  ];
              multiple: boolean;
            }[]
          ];
          batch: boolean;
        };
        state: "closed";
        /**
         * @minItems 0
         * @maxItems 0
         */
        answers: [];
        close_reason: "cancelled" | "expired" | "interrupted";
        created_at: string;
        deadline: string;
        closed_at: string;
      }
  )[];
}
export interface RPCError {
  code: number;
  message: string;
  kind:
    | "INVALID"
    | "NOT_FOUND"
    | "CONFLICT"
    | "BUSY"
    | "LIMIT"
    | "STOPPED"
    | "CLOSED"
    | "IDENTITY"
    | "METHOD"
    | "NETWORK_RESTRICTED"
    | "TERMINAL_WRITE_UNCERTAIN"
    | "ACCOUNT_CREDENTIALS"
    | "ACCOUNT_SETUP"
    | "ACCOUNT_CONFIGURATION"
    | "ACCOUNT_LOGOUT"
    | "ACCOUNT_MANAGEMENT"
    | "PROVIDER_CREDENTIALS"
    | "PROVIDER_DISCOVERY"
    | "PROVIDER_CONFIGURATION"
    | "PROVIDER_KEY_PENDING"
    | "PROVIDER_KEY_STORAGE"
    | "MCP_UNAVAILABLE"
    | "INTERNAL";
}
export interface ReadCompletionParams {
  parent_id: string;
  child_id: string;
  turn_id: string;
  offset: string;
  length: number;
}
export interface ReadCompletionResult {
  completion: {
    parent_id: string;
    child_id: string;
    turn_id: string;
    input_id: null | string;
    message_id: null | string;
    state: "succeeded" | "failed" | "cancelled" | "interrupted";
    failure: null | string;
    mode: "notice" | "inline" | "message";
    finished_at: string;
    text_bytes: string;
    omitted_parts: string;
  };
  offset: string;
  total_bytes: string;
  data_base64: string;
  next_offset: null | string;
}
export interface ReadContentParams {
  session_id: string;
  reference_id: string;
}
export interface ReadContentResult {
  reference: {
    id: string;
    session_id: string;
    digest: string;
    size: string;
    media_type: string;
    created_at: string;
  };
  data_base64: string;
}
export interface ReadHistoryParams {
  session_id: string;
  message_id: string;
  offset: string;
  length: number;
}
export interface ReadHistoryResult {
  message: {
    group_id: string;
    opening_input: boolean;
    source: null | {
      session_id: string;
      message_id: string;
      sequence: string;
    };
    retired_by: null | string;
    retired_revision: null | string;
    id: string;
    session_id: string;
    turn_id: null | string;
    input_id: null | string;
    mail: null | {
      id: string;
      revision: string;
      presentation: "digest" | "body";
    };
    sequence: string;
    role: "system" | "user" | "assistant" | "tool";
    parts_bytes: string;
  };
  offset: string;
  next_offset: null | string;
  data_base64: string;
}
export interface ReadMailParams {
  session_id: string;
  mail_id: string;
}
export interface ReadMailResult {
  mail: {
    id: string;
    revision: string;
    source: {
      kind: "session" | "state" | "completion";
      id: string;
    };
    recipient_id: string;
    delivery: "queued" | "steer" | "next_turn";
    subject: string;
    body_bytes: string;
    evidence_ref: null | string;
    state: "pending" | "delivered" | "done";
    available_at: string;
    created_at: string;
    revised_at: string;
  };
  body: string;
}
export interface ReadStateParams {
  session_id: string;
  version_id: string;
  offset: string;
  length: number;
}
export interface ReadStateResult {
  version: {
    id: string;
    tree_id: string;
    session_id: null | string;
    key: string;
    revision: string;
    author_id: string;
    digest: string;
    size: string;
    created_at: string;
  };
  offset: string;
  data_base64: string;
}
export interface ReadWorkspaceActionParams {
  session_id: string;
  action_id: string;
}
export interface RemoveProviderParams {
  revision: string;
  provider: string;
  replacement: null | {
    selection: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    settings: null | {
      prices: {
        input: null | string;
        output: null | string;
        reasoning: null | string;
        cached_input: null | string;
        cached_output: null | string;
      };
      context_window_tokens: null | string;
      max_output_tokens: string;
      timeout_millis: string;
      max_attempts: number;
    };
  };
}
export interface Request {
  jsonrpc: "2.0";
  id: string;
  method: string;
  params: unknown;
}
export interface RequestIdentity {
  client_id: string;
  request_id: string;
}
export interface ResolvePermissionParams {
  operation_id: string;
  approved: boolean;
}
export interface ResourceUsage {
  session_id: string;
  kind:
    | "depth"
    | "descendants"
    | "queued_inputs"
    | "active_operations"
    | "subscriptions"
    | "runnable_descendants"
    | "schedules";
  revision: string;
  limit: null | string;
  used: string;
}
export interface ResourcesResult {
  items:
    | null
    | {
        session_id: string;
        kind:
          | "depth"
          | "descendants"
          | "queued_inputs"
          | "active_operations"
          | "subscriptions"
          | "runnable_descendants"
          | "schedules";
        revision: string;
        limit: null | string;
        used: string;
      }[];
}
export type Response = {
  jsonrpc: "2.0";
  id: string;
  result?: unknown;
  error?: null | {
    code: number;
    message: string;
    kind:
      | "INVALID"
      | "NOT_FOUND"
      | "CONFLICT"
      | "BUSY"
      | "LIMIT"
      | "STOPPED"
      | "CLOSED"
      | "IDENTITY"
      | "METHOD"
      | "NETWORK_RESTRICTED"
      | "TERMINAL_WRITE_UNCERTAIN"
      | "ACCOUNT_CREDENTIALS"
      | "ACCOUNT_SETUP"
      | "ACCOUNT_CONFIGURATION"
      | "ACCOUNT_LOGOUT"
      | "ACCOUNT_MANAGEMENT"
      | "PROVIDER_CREDENTIALS"
      | "PROVIDER_DISCOVERY"
      | "PROVIDER_CONFIGURATION"
      | "PROVIDER_KEY_PENDING"
      | "PROVIDER_KEY_STORAGE"
      | "MCP_UNAVAILABLE"
      | "INTERNAL";
  };
} & {
  [k: string]: unknown;
};
export interface ResumeGoalParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
  goal: {
    id: string;
    revision: string;
  };
}
export interface RewindParams {
  edit_id: string;
  session_id: string;
  expected_revision: string;
  observed_through: string;
  keep_through: string;
}
export interface RunShellParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
  command: string;
  timeout?: null | number;
  interactive: boolean;
}
export interface ScheduleAdmission {
  id: string;
  schedule: null | {
    id: string;
    session_id: string;
    expression: string;
    first_due: string;
    next_due: null | string;
    cancelled_at: null | string;
    failure: null | string;
    created_at: string;
    parts_bytes: string;
    preview: string;
    preview_truncated: boolean;
    latest: null | {
      schedule_id: string;
      scheduled_for: string;
      input_id: string;
      identity: {
        client_id: string;
        request_id: string;
      };
    };
  };
  deleted_at: null | string;
}
export interface ScheduleParams {
  session_id: string;
  schedule_id: string;
}
export interface ScheduleResult {
  schedule: {
    id: string;
    session_id: string;
    expression: string;
    first_due: string;
    next_due: null | string;
    cancelled_at: null | string;
    failure: null | string;
    created_at: string;
    parts_bytes: string;
    preview: string;
    preview_truncated: boolean;
    latest: null | {
      schedule_id: string;
      scheduled_for: string;
      input_id: string;
      identity: {
        client_id: string;
        request_id: string;
      };
    };
  };
  /**
   * @minItems 1
   * @maxItems 128
   */
  parts: [
    (
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
      | {
          call: {
            arguments: {
              [k: string]: unknown;
            };
            id: string;
            name: string;
          };
          type: "tool_call";
        }
      | {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        }
    ),
    ...(
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
      | {
          call: {
            arguments: {
              [k: string]: unknown;
            };
            id: string;
            name: string;
          };
          type: "tool_call";
        }
      | {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        }
    )[]
  ];
}
export interface SchedulesResult {
  items:
    | null
    | {
        id: string;
        session_id: string;
        expression: string;
        first_due: string;
        next_due: null | string;
        cancelled_at: null | string;
        failure: null | string;
        created_at: string;
        parts_bytes: string;
        preview: string;
        preview_truncated: boolean;
        latest: null | {
          schedule_id: string;
          scheduled_for: string;
          input_id: string;
          identity: {
            client_id: string;
            request_id: string;
          };
        };
      }[];
  next_after: null | string;
  next_cursor: null | {
    due: string;
    id: string;
  };
}
export interface SearchHistoryParams {
  expected_revision?: null | string;
  session_id: string;
  after: string;
  through_sequence: string;
  query: string;
  limit: number;
}
export interface SearchHistoryResult {
  revision: string;
  matches:
    | null
    | {
        message: {
          group_id: string;
          opening_input: boolean;
          source: null | {
            session_id: string;
            message_id: string;
            sequence: string;
          };
          retired_by: null | string;
          retired_revision: null | string;
          id: string;
          session_id: string;
          turn_id: null | string;
          input_id: null | string;
          mail: null | {
            id: string;
            revision: string;
            presentation: "digest" | "body";
          };
          sequence: string;
          role: "system" | "user" | "assistant" | "tool";
          parts_bytes: string;
        };
        part_index: number;
        field: "text" | "arguments" | "output";
        offset: string;
        snippet: string;
        truncated: boolean;
      }[];
  through_sequence: string;
  next_after: null | string;
  scanned_messages: string;
  scanned_bytes: string;
}
export interface SelectCompactionParams {
  session_id: string;
  expected_revision: string;
  compaction_id: null | string;
}
export interface SendMailParams {
  mail_id: string;
  sender_id: string;
  recipient_id: string;
  delivery: "queued" | "steer" | "next_turn";
  subject: string;
  body: string;
  evidence_ref?: null | string;
  available_at?: null | string;
}
export interface Session {
  history_revision: string;
  id: string;
  tree_id: string;
  parent_id: null | string;
  definition: {
    id: string;
    revision: string;
  };
  config_revision: string;
  configuration: {
    mcp_servers: {
      [k: string]: unknown;
    };
    /**
     * @maxItems 17
     */
    modules: (
      | "agents"
      | "artifacts"
      | "browser"
      | "computer"
      | "context"
      | "files"
      | "goals"
      | "mail"
      | "mcp"
      | "messages"
      | "models"
      | "permissions"
      | "schedules"
      | "shell"
      | "skills"
      | "state"
      | "user"
    )[];
    tools_definition: null | {
      id: string;
      revision: string;
    };
    hooks_definition: null | {
      id: string;
      revision: string;
    };
    automatic_title: boolean;
    goals_enabled: boolean;
    compaction: {
      model: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      threshold_percent: number;
    };
    report_mode: "notice" | "inline" | "message";
    model:
      | {
          provider: string;
          name: string;
          effort: string;
          temperature?: null | number;
          top_p?: null | number;
        }
      | {
          provider: "";
          name: "";
          effort: "";
          temperature?: null | number;
          top_p?: null | number;
        };
    instructions: {
      project_root: null | string;
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
      standing_instructions: boolean;
      skill_roots: null | string[];
    };
    tools: {
      [k: string]: {
        timeout_millis: number;
        description: string;
        input_schema: unknown;
        output_schema: unknown;
      };
    } | null;
    children: {
      [k: string]: {
        id: string;
        revision: string;
      };
    } | null;
    hooks: {
      [k: string]: {
        operations: null | string[];
        optional: boolean;
        timeout_millis: number;
      };
    } | null;
    output_schema: unknown;
  };
  working_directory: string;
  lifecycle: "active" | "stopped";
  created_at: string;
}
export interface SessionActivity {
  session_id: string;
  lifecycle: "active" | "stopped";
  active_turn: null | {
    history_revision: string;
    goal: null | {
      id: string;
      revision: string;
    };
    id: string;
    session_id: string;
    kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
    config_revision: string;
    state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
    failure: null | string;
    started_at: string;
    finished_at: null | string;
  };
  active_input_id: null | string;
  queued_input_count: string;
  pending_permission_count: string;
  pending_question_count: string;
  execution_permit: boolean;
  active_workspace_action_id: null | string;
}
export interface SessionInputParams {
  session_id: string;
  input_id: string;
}
export interface SessionObservation {
  snapshot: {
    revision: string;
    session_id: string;
    through_sequence: string;
    message_count: string;
  };
  epoch: string;
  messages:
    | null
    | (
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "user";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "system";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "assistant";
            /**
             * @minItems 1
             * @maxItems 128
             */
            parts: [
              (
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              ),
              ...(
                | {
                    text: string;
                    type: "text";
                  }
                | {
                    reference_id: string;
                    type: "content";
                  }
                | {
                    call: {
                      arguments: {
                        [k: string]: unknown;
                      };
                      id: string;
                      name: string;
                    };
                    type: "tool_call";
                  }
              )[]
            ];
            created_at: string;
          }
        | {
            group_id: string;
            opening_input: boolean;
            source: null | {
              session_id: string;
              message_id: string;
              sequence: string;
            };
            retired_by: null | string;
            retired_revision: null | string;
            id: string;
            session_id: string;
            turn_id: null | string;
            input_id: null | string;
            mail: null | {
              id: string;
              revision: string;
              presentation: "digest" | "body";
            };
            sequence: string;
            role: "tool";
            /**
             * @minItems 1
             * @maxItems 9
             */
            parts: [
              {
                result: {
                  call_id: string;
                  is_error: boolean;
                  output: string;
                };
                type: "tool_result";
              },
              ...{
                reference_id: string;
                type: "content";
              }[]
            ];
            created_at: string;
          }
      )[];
  preview: null | {
    attempt_id: string;
    turn_id: string;
    message_id: string;
    revision: string;
    text: string;
    reasoning: string;
    calls:
      | null
      | {
          index: number;
          id: string;
          name: string;
          arguments: string;
        }[];
    truncated: boolean;
  };
}
export interface SessionParams {
  session_id: string;
}
export interface SetBudgetParams {
  session_id: string;
  expected_revision: string;
  budget: {
    kind:
      | "model_calls"
      | "model_tokens"
      | "model_cost_nano_usd"
      | "model_elapsed_millis"
      | "logical_writes"
      | "logical_write_bytes";
    limit: null | string;
  };
}
export interface SetDefaultPermissionModeParams {
  expected_revision: string;
  mode: "prompt" | "automatic";
}
export interface SetPermissionModeParams {
  edit_id: string;
  session_id: string;
  expected_revision: string;
  mode: "prompt" | "automatic";
}
export interface SetResourceParams {
  session_id: string;
  expected_revision: string;
  resource: {
    kind:
      | "depth"
      | "descendants"
      | "queued_inputs"
      | "active_operations"
      | "subscriptions"
      | "runnable_descendants"
      | "schedules";
    limit: null | string;
  };
}
export interface ShellInputParams {
  session_id: string;
  operation_id: string;
  sequence: string;
  data_base64: string;
}
export interface ShellInputResult {
  sequence: string;
}
export interface ShellInteractionParams {
  session_id: string;
  cursor: string;
}
export interface ShellInteractionResult {
  interaction: null | {
    operation_id: string;
    started_at: string;
    data_base64: string;
    from: string;
    through: string;
    next_input: string;
    seconds_left: number;
  };
}
export interface SpawnSessionParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  parent_id: string;
  definition?: null | {
    id: string;
    revision: string;
  };
  overrides: {
    mcp_servers?: {
      [k: string]: unknown;
    } & (null | {
      all: boolean;
      /**
       * @maxItems 64
       */
      servers: string[];
    });
    /**
     * @maxItems 17
     */
    modules?:
      | null
      | (
          | "agents"
          | "artifacts"
          | "browser"
          | "computer"
          | "context"
          | "files"
          | "goals"
          | "mail"
          | "mcp"
          | "messages"
          | "models"
          | "permissions"
          | "schedules"
          | "shell"
          | "skills"
          | "state"
          | "user"
        )[];
    automatic_title?: null | boolean;
    goals_enabled?: null | boolean;
    compaction?: null | {
      model: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      threshold_percent: number;
    };
    report_mode?: null | ("notice" | "inline" | "message");
    model?: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    instructions?: null | {
      project_root: null | string;
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
      standing_instructions: boolean;
      skill_roots: null | string[];
    };
    tools?: {
      [k: string]: {
        timeout_millis: number;
        description: string;
        input_schema: unknown;
        output_schema: unknown;
      };
    } | null;
    children?: {
      [k: string]: {
        id: string;
        revision: string;
      };
    } | null;
    hooks?: {
      [k: string]: {
        operations: null | string[];
        optional: boolean;
        timeout_millis: number;
      };
    } | null;
    output?: null | {
      schema: unknown;
    };
  };
  working_directory?: null | string;
  /**
   * @minItems 1
   * @maxItems 128
   */
  parts: [
    (
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
    ),
    ...(
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
    )[]
  ];
  grant_ids: null | string[];
  budgets?:
    | null
    | {
        kind:
          | "model_calls"
          | "model_tokens"
          | "model_cost_nano_usd"
          | "model_elapsed_millis"
          | "logical_writes"
          | "logical_write_bytes";
        limit: null | string;
      }[];
  resources?:
    | null
    | {
        kind:
          | "depth"
          | "descendants"
          | "queued_inputs"
          | "active_operations"
          | "subscriptions"
          | "runnable_descendants"
          | "schedules";
        limit: null | string;
      }[];
}
export interface SpawnSessionResult {
  session: null | {
    history_revision: string;
    id: string;
    tree_id: string;
    parent_id: null | string;
    definition: {
      id: string;
      revision: string;
    };
    config_revision: string;
    configuration: {
      mcp_servers: {
        [k: string]: unknown;
      };
      /**
       * @maxItems 17
       */
      modules: (
        | "agents"
        | "artifacts"
        | "browser"
        | "computer"
        | "context"
        | "files"
        | "goals"
        | "mail"
        | "mcp"
        | "messages"
        | "models"
        | "permissions"
        | "schedules"
        | "shell"
        | "skills"
        | "state"
        | "user"
      )[];
      tools_definition: null | {
        id: string;
        revision: string;
      };
      hooks_definition: null | {
        id: string;
        revision: string;
      };
      automatic_title: boolean;
      goals_enabled: boolean;
      compaction: {
        model: null | {
          provider: string;
          name: string;
          effort: string;
          temperature?: null | number;
          top_p?: null | number;
        };
        threshold_percent: number;
      };
      report_mode: "notice" | "inline" | "message";
      model:
        | {
            provider: string;
            name: string;
            effort: string;
            temperature?: null | number;
            top_p?: null | number;
          }
        | {
            provider: "";
            name: "";
            effort: "";
            temperature?: null | number;
            top_p?: null | number;
          };
      instructions: {
        project_root: null | string;
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
        standing_instructions: boolean;
        skill_roots: null | string[];
      };
      tools: {
        [k: string]: {
          timeout_millis: number;
          description: string;
          input_schema: unknown;
          output_schema: unknown;
        };
      } | null;
      children: {
        [k: string]: {
          id: string;
          revision: string;
        };
      } | null;
      hooks: {
        [k: string]: {
          operations: null | string[];
          optional: boolean;
          timeout_millis: number;
        };
      } | null;
      output_schema: unknown;
    };
    working_directory: string;
    lifecycle: "active" | "stopped";
    created_at: string;
  };
  admission: {
    receipt: {
      identity: {
        client_id: string;
        request_id: string;
      };
      digest: string;
      input_id: null | string;
      deleted_at: null | string;
      created_at: string;
    };
    input:
      | {
          host_operation: null;
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "prompt";
          /**
           * @minItems 1
           * @maxItems 128
           */
          parts: [
            (
              | {
                  text: string;
                  type: "text";
                }
              | {
                  reference_id: string;
                  type: "content";
                }
            ),
            ...(
              | {
                  text: string;
                  type: "text";
                }
              | {
                  reference_id: string;
                  type: "content";
                }
            )[]
          ];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | {
          host_operation: null;
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "compact" | "goal_formulation" | "automatic_title";
          /**
           * @maxItems 0
           */
          parts: [];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | {
          host_operation: {
            module: "shell" | "files" | "tools" | "computer";
            name: string;
            arguments_base64: string;
          };
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user";
          kind: "host_operation";
          /**
           * @maxItems 0
           */
          parts: [];
          state: "queued" | "claimed" | "cancelled";
          turn_id: null | string;
          created_at: string;
          schedule: null | {
            schedule_id: string;
            scheduled_for: string;
          };
        }
      | null;
    turn: null | {
      history_revision: string;
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
      config_revision: string;
      state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
      failure: null | string;
      started_at: string;
      finished_at: null | string;
    };
  };
}
export interface StateHistoryParams {
  session_id: string;
  scope: "session" | "tree";
  key: string;
  after: string;
  limit: number;
}
export interface StateSubscription {
  id: string;
  tree_id: string;
  session_id: string;
  key: string;
  delivery: "queued" | "steer" | "next_turn";
  cursor: string;
  cancelled_at: null | string;
  created_at: string;
}
export interface StateSubscriptionsParams {
  session_id: string;
  after?: null | string;
  limit: number;
}
export interface StateSubscriptionsResult {
  items:
    | null
    | {
        id: string;
        tree_id: string;
        session_id: string;
        key: string;
        delivery: "queued" | "steer" | "next_turn";
        cursor: string;
        cancelled_at: null | string;
        created_at: string;
      }[];
}
export interface StateVersion {
  id: string;
  tree_id: string;
  session_id: null | string;
  key: string;
  revision: string;
  author_id: string;
  digest: string;
  size: string;
  created_at: string;
}
export interface StateVersionsResult {
  items:
    | null
    | {
        id: string;
        tree_id: string;
        session_id: null | string;
        key: string;
        revision: string;
        author_id: string;
        digest: string;
        size: string;
        created_at: string;
      }[];
}
export interface SubmitParams {
  identity: {
    client_id: string;
    request_id: string;
  };
  session_id: string;
  source: "user" | "agent";
  /**
   * @minItems 1
   * @maxItems 128
   */
  parts: [
    (
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
    ),
    ...(
      | {
          text: string;
          type: "text";
        }
      | {
          reference_id: string;
          type: "content";
        }
    )[]
  ];
}
export interface SubscribeStateParams {
  subscription_id: string;
  session_id: string;
  key: string;
  after: string;
  delivery: "queued" | "steer" | "next_turn";
}
export interface TerminalAccepted {
  accepted: boolean;
}
export interface TerminalInfo {
  process_epoch: string;
  id: string;
  cwd: string;
  shell: string;
  cols: number;
  rows: number;
  closing: boolean;
  exited: boolean;
  exit_code: number;
  signal: string;
  start: string;
  end: string;
  created_at: string;
}
export interface TerminalList {
  process_epoch: string;
  /**
   * @maxItems 16
   */
  items: {
    process_epoch: string;
    id: string;
    cwd: string;
    shell: string;
    cols: number;
    rows: number;
    closing: boolean;
    exited: boolean;
    exit_code: number;
    signal: string;
    start: string;
    end: string;
    created_at: string;
  }[];
}
export interface TerminalListParams {
  process_epoch: string;
}
export interface TerminalOpenParams {
  process_epoch: string;
  cwd: string;
  cols: number;
  rows: number;
}
export interface TerminalPage {
  terminal: {
    process_epoch: string;
    id: string;
    cwd: string;
    shell: string;
    cols: number;
    rows: number;
    closing: boolean;
    exited: boolean;
    exit_code: number;
    signal: string;
    start: string;
    end: string;
    created_at: string;
  };
  from: string;
  next: string;
  end: string;
  truncated: boolean;
  data_base64: string;
}
export interface TerminalReadParams {
  process_epoch: string;
  id: string;
  cursor: string;
  limit: number;
}
export interface TerminalRef {
  process_epoch: string;
  id: string;
}
export interface TerminalResizeParams {
  process_epoch: string;
  id: string;
  cols: number;
  rows: number;
}
export interface TerminalWriteParams {
  process_epoch: string;
  id: string;
  data_base64: string;
}
export interface ToolCall {
  arguments: {
    [k: string]: unknown;
  };
  id: string;
  name: string;
}
export interface ToolResult {
  call_id: string;
  is_error: boolean;
  output: string;
}
export interface Tree {
  id: string;
  metadata: {
    title: null | string;
    archived: boolean;
    pinned: boolean;
  };
  engine: "starlark" | "quickjs";
  revision: string;
  created_at: string;
}
export interface TreeCatalog {
  revision: string;
}
export interface TreeCreationParams {
  creation_id: string;
}
export interface TreeParams {
  tree_id: string;
}
export interface Turn {
  history_revision: string;
  goal: null | {
    id: string;
    revision: string;
  };
  id: string;
  session_id: string;
  kind: "prompt" | "compact" | "goal_formulation" | "automatic_title" | "host_operation";
  config_revision: string;
  state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
  failure: null | string;
  started_at: string;
  finished_at: null | string;
}
export interface TurnOutputResult {
  output: null | {
    turn_id: string;
    message_id: string;
    data_base64: string;
  };
}
export interface TurnParams {
  turn_id: string;
}
export interface UnsubscribeStateParams {
  session_id: string;
  subscription_id: string;
}
export interface UpdateConfigurationParams {
  session_id: string;
  expected_revision: string;
  patch: {
    mcp_servers?: {
      [k: string]: unknown;
    } & (null | {
      all: boolean;
      /**
       * @maxItems 64
       */
      servers: string[];
    });
    /**
     * @maxItems 17
     */
    modules?:
      | null
      | (
          | "agents"
          | "artifacts"
          | "browser"
          | "computer"
          | "context"
          | "files"
          | "goals"
          | "mail"
          | "mcp"
          | "messages"
          | "models"
          | "permissions"
          | "schedules"
          | "shell"
          | "skills"
          | "state"
          | "user"
        )[];
    automatic_title?: null | boolean;
    goals_enabled?: null | boolean;
    compaction?: null | {
      model: null | {
        provider: string;
        name: string;
        effort: string;
        temperature?: null | number;
        top_p?: null | number;
      };
      threshold_percent: number;
    };
    report_mode?: null | ("notice" | "inline" | "message");
    model?: null | {
      provider: string;
      name: string;
      effort: string;
      temperature?: null | number;
      top_p?: null | number;
    };
    instructions?: null | {
      project_root: null | string;
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
      standing_instructions: boolean;
      skill_roots: null | string[];
    };
    tools?: {
      [k: string]: {
        timeout_millis: number;
        description: string;
        input_schema: unknown;
        output_schema: unknown;
      };
    } | null;
    children?: {
      [k: string]: {
        id: string;
        revision: string;
      };
    } | null;
    hooks?: {
      [k: string]: {
        operations: null | string[];
        optional: boolean;
        timeout_millis: number;
      };
    } | null;
    output?: null | {
      schema: unknown;
    };
  };
}
export interface UpdateTreeParams {
  tree_id: string;
  expected_revision: string;
  metadata: {
    title: null | string;
    archived: boolean;
    pinned: boolean;
  };
}
export interface WorkspaceAction {
  id: string;
  session_id: string;
  snapshot_id: string;
  kind: "capture" | "restore" | "release";
  state: "claimed" | "succeeded" | "uncertain";
  failure: null | string;
  created_at: string;
  finished_at: null | string;
}
export interface WorkspaceActionParams {
  action_id: string;
  snapshot_id: string;
  session_id: string;
}
export interface WorkspaceResult {
  action: {
    id: string;
    session_id: string;
    snapshot_id: string;
    kind: "capture" | "restore" | "release";
    state: "claimed" | "succeeded" | "uncertain";
    failure: null | string;
    created_at: string;
    finished_at: null | string;
  };
  snapshot: {
    id: string;
    session_id: string;
    capture_id: string;
    state: "claimed" | "succeeded" | "uncertain";
    scope: "session_working_directory";
    semantics: string;
    created_at: string;
    released_at: null | string;
  };
}
export interface WorkspaceSnapshot {
  id: string;
  session_id: string;
  capture_id: string;
  state: "claimed" | "succeeded" | "uncertain";
  scope: "session_working_directory";
  semantics: string;
  created_at: string;
  released_at: null | string;
}
export interface WorkspaceSnapshotParams {
  session_id: string;
  snapshot_id: string;
}
export interface WorkspaceSnapshotsParams {
  session_id: string;
  after?: string;
  limit: number;
}
export interface WorkspaceSnapshotsResult {
  /**
   * @maxItems 100
   */
  items: {
    id: string;
    session_id: string;
    capture_id: string;
    state: "claimed" | "succeeded" | "uncertain";
    scope: "session_working_directory";
    semantics: string;
    created_at: string;
    released_at: null | string;
  }[];
}
export interface WriteStateParams {
  session_id: string;
  scope: "session" | "tree";
  version_id: string;
  key: string;
  expected_revision: string;
  data_base64: string;
}

export interface ContractTypes {
  Admission: Admission;
  AnswerQuestionParams: AnswerQuestionParams;
  AutomaticTitleDecision: AutomaticTitleDecision;
  AutomaticTitleResult: AutomaticTitleResult;
  AutomaticTitleResultParams: AutomaticTitleResultParams;
  Budget: Budget;
  BudgetsResult: BudgetsResult;
  CallHostToolParams: CallHostToolParams;
  Cell: Cell;
  CellParams: CellParams;
  CellsParams: CellsParams;
  CellsResult: CellsResult;
  ChangeProviderParams: ChangeProviderParams;
  CompactParams: CompactParams;
  CompactionParams: CompactionParams;
  CompactionResult: CompactionResult;
  CompactionsParams: CompactionsParams;
  CompactionsResult: CompactionsResult;
  ComputerConnectionParams: ComputerConnectionParams;
  ComputerStatus: ComputerStatus;
  ConfigureComputerParams: ConfigureComputerParams;
  ConfigureMCPParams: ConfigureMCPParams;
  ContentReference: ContentReference;
  ContextHead: ContextHead;
  ContextHistoryParams: ContextHistoryParams;
  CreateGoalParams: CreateGoalParams;
  CreateGrantParams: CreateGrantParams;
  CreateScheduleParams: CreateScheduleParams;
  CreateTreeParams: CreateTreeParams;
  CreateTreeResult: CreateTreeResult;
  CurrentGoalResult: CurrentGoalResult;
  DefaultPermissionMode: DefaultPermissionMode;
  Definition: Definition;
  DefinitionDocument: DefinitionDocument;
  DefinitionRef: DefinitionRef;
  DeleteResult: DeleteResult;
  EmptyParams: EmptyParams;
  ExecutorAccepted: ExecutorAccepted;
  ExecutorActivityResult: ExecutorActivityResult;
  ExecutorBindParams: ExecutorBindParams;
  ExecutorEvent: ExecutorEvent;
  ExecutorHookResultParams: ExecutorHookResultParams;
  ExecutorLease: ExecutorLease;
  ExecutorPendingParams: ExecutorPendingParams;
  ExecutorPendingResult: ExecutorPendingResult;
  ExecutorProgressParams: ExecutorProgressParams;
  ExecutorToolResultParams: ExecutorToolResultParams;
  ForkParams: ForkParams;
  ForkResult: ForkResult;
  FormulateGoalParams: FormulateGoalParams;
  GetStateParams: GetStateParams;
  Goal: Goal;
  GoalAdmission: GoalAdmission;
  GoalChange: GoalChange;
  GoalFormulation: GoalFormulation;
  GoalFormulationParams: GoalFormulationParams;
  GoalParams: GoalParams;
  Grant: Grant;
  GrantParams: GrantParams;
  GrantsParams: GrantsParams;
  GrantsResult: GrantsResult;
  HistoryEdit: HistoryEdit;
  HistoryMetadataResult: HistoryMetadataResult;
  HistoryPageParams: HistoryPageParams;
  HistoryPageResult: HistoryPageResult;
  HistoryParams: HistoryParams;
  HistoryResult: HistoryResult;
  HistorySnapshot: HistorySnapshot;
  HostAttentionParams: HostAttentionParams;
  HostAttentionResult: HostAttentionResult;
  HostDirectoriesParams: HostDirectoriesParams;
  HostDirectoriesResult: HostDirectoriesResult;
  HostDirectoryPickParams: HostDirectoryPickParams;
  HostDirectoryPickResult: HostDirectoryPickResult;
  HostOperation: HostOperation;
  HostOperationParams: HostOperationParams;
  HostOperationsParams: HostOperationsParams;
  HostOperationsResult: HostOperationsResult;
  HostSkillsParams: HostSkillsParams;
  HostSkillsResult: HostSkillsResult;
  HostThemeResolveParams: HostThemeResolveParams;
  HostThemeResolved: HostThemeResolved;
  HostThemesResult: HostThemesResult;
  HostToolSchemasResult: HostToolSchemasResult;
  InferenceAccountStatus: InferenceAccountStatus;
  InferenceCleanupResult: InferenceCleanupResult;
  InferenceCreateProjectParams: InferenceCreateProjectParams;
  InferenceFlow: InferenceFlow;
  InferenceFlowParams: InferenceFlowParams;
  InferenceFlowsResult: InferenceFlowsResult;
  InferenceLogoutResult: InferenceLogoutResult;
  InferenceProjectParams: InferenceProjectParams;
  InferenceTeamParams: InferenceTeamParams;
  InitializeParams: InitializeParams;
  InitializeResult: InitializeResult;
  Input: Input;
  InputPageParams: InputPageParams;
  InputPageResult: InputPageResult;
  InputParams: InputParams;
  InstructionManifestResult: InstructionManifestResult;
  LanguageServersResult: LanguageServersResult;
  LifecycleParams: LifecycleParams;
  ListCompletionsParams: ListCompletionsParams;
  ListCompletionsResult: ListCompletionsResult;
  ListDefinitionsParams: ListDefinitionsParams;
  ListDefinitionsResult: ListDefinitionsResult;
  ListMailParams: ListMailParams;
  ListMailResult: ListMailResult;
  ListSchedulesParams: ListSchedulesParams;
  ListSessionsParams: ListSessionsParams;
  ListSessionsResult: ListSessionsResult;
  ListSkillsParams: ListSkillsParams;
  ListSkillsResult: ListSkillsResult;
  ListStateParams: ListStateParams;
  ListTreesParams: ListTreesParams;
  ListTreesResult: ListTreesResult;
  MCPAttachParams: MCPAttachParams;
  MCPBrandIconsParams: MCPBrandIconsParams;
  MCPBrandIconsResult: MCPBrandIconsResult;
  MCPConfiguration: MCPConfiguration;
  MCPImportCandidatesParams: MCPImportCandidatesParams;
  MCPImportCandidatesResult: MCPImportCandidatesResult;
  MCPImportParams: MCPImportParams;
  MCPImportResult: MCPImportResult;
  MCPInstructionsResult: MCPInstructionsResult;
  MCPRefreshResult: MCPRefreshResult;
  MCPServerParams: MCPServerParams;
  MCPStatusResult: MCPStatusResult;
  MCPToolsResult: MCPToolsResult;
  MailAdmission: MailAdmission;
  MatchReceiptParams: MatchReceiptParams;
  Message: Message;
  ModelAttemptsParams: ModelAttemptsParams;
  ModelAttemptsResult: ModelAttemptsResult;
  OpenAIAccountStatus: OpenAIAccountStatus;
  OpenAIFlowParams: OpenAIFlowParams;
  OpenAIFlowsResult: OpenAIFlowsResult;
  OpenAILoginFlow: OpenAILoginFlow;
  Part: Part;
  Permission: Permission;
  PermissionModeEdit: PermissionModeEdit;
  PermissionModeEditParams: PermissionModeEditParams;
  PermissionPolicy: PermissionPolicy;
  PermissionsParams: PermissionsParams;
  PermissionsResult: PermissionsResult;
  ProviderCatalog: ProviderCatalog;
  ProviderDefaultsParams: ProviderDefaultsParams;
  ProviderInventory: ProviderInventory;
  ProviderModelsResult: ProviderModelsResult;
  ProviderParams: ProviderParams;
  ProviderPresetsResult: ProviderPresetsResult;
  ProviderReadiness: ProviderReadiness;
  ProviderReadinessParams: ProviderReadinessParams;
  PutContentParams: PutContentParams;
  Question: Question;
  QuestionParams: QuestionParams;
  QuestionsParams: QuestionsParams;
  QuestionsResult: QuestionsResult;
  RPCError: RPCError;
  ReadCompletionParams: ReadCompletionParams;
  ReadCompletionResult: ReadCompletionResult;
  ReadContentParams: ReadContentParams;
  ReadContentResult: ReadContentResult;
  ReadHistoryParams: ReadHistoryParams;
  ReadHistoryResult: ReadHistoryResult;
  ReadMailParams: ReadMailParams;
  ReadMailResult: ReadMailResult;
  ReadStateParams: ReadStateParams;
  ReadStateResult: ReadStateResult;
  ReadWorkspaceActionParams: ReadWorkspaceActionParams;
  RemoveProviderParams: RemoveProviderParams;
  Request: Request;
  RequestIdentity: RequestIdentity;
  ResolvePermissionParams: ResolvePermissionParams;
  ResourceUsage: ResourceUsage;
  ResourcesResult: ResourcesResult;
  Response: Response;
  ResumeGoalParams: ResumeGoalParams;
  RewindParams: RewindParams;
  RunShellParams: RunShellParams;
  ScheduleAdmission: ScheduleAdmission;
  ScheduleParams: ScheduleParams;
  ScheduleResult: ScheduleResult;
  SchedulesResult: SchedulesResult;
  SearchHistoryParams: SearchHistoryParams;
  SearchHistoryResult: SearchHistoryResult;
  SelectCompactionParams: SelectCompactionParams;
  SendMailParams: SendMailParams;
  Session: Session;
  SessionActivity: SessionActivity;
  SessionInputParams: SessionInputParams;
  SessionObservation: SessionObservation;
  SessionParams: SessionParams;
  SetBudgetParams: SetBudgetParams;
  SetDefaultPermissionModeParams: SetDefaultPermissionModeParams;
  SetPermissionModeParams: SetPermissionModeParams;
  SetResourceParams: SetResourceParams;
  ShellInputParams: ShellInputParams;
  ShellInputResult: ShellInputResult;
  ShellInteractionParams: ShellInteractionParams;
  ShellInteractionResult: ShellInteractionResult;
  SpawnSessionParams: SpawnSessionParams;
  SpawnSessionResult: SpawnSessionResult;
  StateHistoryParams: StateHistoryParams;
  StateSubscription: StateSubscription;
  StateSubscriptionsParams: StateSubscriptionsParams;
  StateSubscriptionsResult: StateSubscriptionsResult;
  StateVersion: StateVersion;
  StateVersionsResult: StateVersionsResult;
  SubmitParams: SubmitParams;
  SubscribeStateParams: SubscribeStateParams;
  TerminalAccepted: TerminalAccepted;
  TerminalInfo: TerminalInfo;
  TerminalList: TerminalList;
  TerminalListParams: TerminalListParams;
  TerminalOpenParams: TerminalOpenParams;
  TerminalPage: TerminalPage;
  TerminalReadParams: TerminalReadParams;
  TerminalRef: TerminalRef;
  TerminalResizeParams: TerminalResizeParams;
  TerminalWriteParams: TerminalWriteParams;
  ToolCall: ToolCall;
  ToolResult: ToolResult;
  Tree: Tree;
  TreeCatalog: TreeCatalog;
  TreeCreationParams: TreeCreationParams;
  TreeParams: TreeParams;
  Turn: Turn;
  TurnOutputResult: TurnOutputResult;
  TurnParams: TurnParams;
  UnsubscribeStateParams: UnsubscribeStateParams;
  UpdateConfigurationParams: UpdateConfigurationParams;
  UpdateTreeParams: UpdateTreeParams;
  WorkspaceAction: WorkspaceAction;
  WorkspaceActionParams: WorkspaceActionParams;
  WorkspaceResult: WorkspaceResult;
  WorkspaceSnapshot: WorkspaceSnapshot;
  WorkspaceSnapshotParams: WorkspaceSnapshotParams;
  WorkspaceSnapshotsParams: WorkspaceSnapshotsParams;
  WorkspaceSnapshotsResult: WorkspaceSnapshotsResult;
  WriteStateParams: WriteStateParams;
}
export interface Operations {
  "host.attention": { params: HostAttentionParams; result: HostAttentionResult };
  "host.directories.list": { params: HostDirectoriesParams; result: HostDirectoriesResult };
  "host.directory.pick": { params: HostDirectoryPickParams; result: HostDirectoryPickResult };
  "host.skills.complete": { params: HostSkillsParams; result: HostSkillsResult };
  "host.themes.list": { params: EmptyParams; result: HostThemesResult };
  "host.themes.resolve": { params: HostThemeResolveParams; result: HostThemeResolved };
  "tool.schemas": { params: SessionParams; result: HostToolSchemasResult };
  "tool.call": { params: CallHostToolParams; result: Admission };
  "shell.run": { params: RunShellParams; result: Admission };
  "executor.activity": { params: SessionParams; result: ExecutorActivityResult };
  "executor.bind": { params: ExecutorBindParams; result: ExecutorLease };
  "executor.pending": { params: ExecutorPendingParams; result: ExecutorPendingResult };
  "tool.result": { params: ExecutorToolResultParams; result: ExecutorAccepted };
  "hook.result": { params: ExecutorHookResultParams; result: ExecutorAccepted };
  "tool.progress": { params: ExecutorProgressParams; result: ExecutorAccepted };
  "shell.interaction": { params: ShellInteractionParams; result: ShellInteractionResult };
  "shell.input": { params: ShellInputParams; result: ShellInputResult };
  "computer.status": { params: EmptyParams; result: ComputerStatus };
  "computer.configure": { params: ConfigureComputerParams; result: ComputerStatus };
  "computer.reconnect": { params: ComputerConnectionParams; result: ComputerStatus };
  "computer.disconnect": { params: ComputerConnectionParams; result: ComputerStatus };
  "mcp.configuration": { params: EmptyParams; result: MCPConfiguration };
  "mcp.configure": { params: ConfigureMCPParams; result: MCPConfiguration };
  "mcp.import.candidates": { params: MCPImportCandidatesParams; result: MCPImportCandidatesResult };
  "mcp.import.apply": { params: MCPImportParams; result: MCPImportResult };
  "mcp.status": { params: SessionParams; result: MCPStatusResult };
  "mcp.refresh": { params: SessionParams; result: MCPRefreshResult };
  "mcp.reload": { params: SessionParams; result: MCPRefreshResult };
  "mcp.reconnect": { params: MCPServerParams; result: MCPRefreshResult };
  "mcp.enable": { params: MCPServerParams; result: MCPRefreshResult };
  "mcp.disable": { params: MCPServerParams; result: MCPRefreshResult };
  "mcp.attach": { params: MCPAttachParams; result: MCPRefreshResult };
  "mcp.tools": { params: MCPServerParams; result: MCPToolsResult };
  "mcp.instructions": { params: MCPServerParams; result: MCPInstructionsResult };
  "mcp.brand.icons": { params: MCPBrandIconsParams; result: MCPBrandIconsResult };
  "terminal.open": { params: TerminalOpenParams; result: TerminalInfo };
  "terminal.list": { params: TerminalListParams; result: TerminalList };
  "terminal.read": { params: TerminalReadParams; result: TerminalPage };
  "terminal.write": { params: TerminalWriteParams; result: TerminalAccepted };
  "terminal.resize": { params: TerminalResizeParams; result: TerminalInfo };
  "terminal.close": { params: TerminalRef; result: TerminalAccepted };
  "workspace.capture": { params: WorkspaceActionParams; result: WorkspaceResult };
  "workspace.restore": { params: WorkspaceActionParams; result: WorkspaceResult };
  "workspace.release": { params: WorkspaceActionParams; result: WorkspaceResult };
  "workspace.action": { params: ReadWorkspaceActionParams; result: WorkspaceAction };
  "workspace.snapshot": { params: WorkspaceSnapshotParams; result: WorkspaceSnapshot };
  "workspace.snapshots": { params: WorkspaceSnapshotsParams; result: WorkspaceSnapshotsResult };
  "providers.presets": { params: EmptyParams; result: ProviderPresetsResult };
  "providers.bundled": { params: ProviderParams; result: ProviderModelsResult };
  "providers.list": { params: EmptyParams; result: ProviderInventory };
  "providers.create": { params: ChangeProviderParams; result: ProviderInventory };
  "providers.update": { params: ChangeProviderParams; result: ProviderInventory };
  "providers.remove": { params: RemoveProviderParams; result: ProviderInventory };
  "providers.defaults": { params: ProviderDefaultsParams; result: ProviderInventory };
  "providers.compaction": { params: ProviderDefaultsParams; result: ProviderInventory };
  "providers.catalog": { params: ProviderParams; result: ProviderCatalog };
  "providers.refresh": { params: ProviderParams; result: ProviderCatalog };
  "providers.readiness": { params: ProviderReadinessParams; result: ProviderReadiness };
  "lsp.status": { params: SessionParams; result: LanguageServersResult };
  "accounts.openai.begin": { params: EmptyParams; result: OpenAILoginFlow };
  "accounts.openai.get": { params: OpenAIFlowParams; result: OpenAILoginFlow };
  "accounts.openai.list": { params: EmptyParams; result: OpenAIFlowsResult };
  "accounts.openai.cancel": { params: OpenAIFlowParams; result: OpenAILoginFlow };
  "accounts.openai.status": { params: EmptyParams; result: OpenAIAccountStatus };
  "accounts.openai.setup": { params: EmptyParams; result: OpenAIAccountStatus };
  "accounts.openai.logout": { params: EmptyParams; result: OpenAIAccountStatus };
  "accounts.inference.begin": { params: EmptyParams; result: InferenceFlow };
  "accounts.inference.get": { params: InferenceFlowParams; result: InferenceFlow };
  "accounts.inference.list": { params: EmptyParams; result: InferenceFlowsResult };
  "accounts.inference.cancel": { params: InferenceFlowParams; result: InferenceFlow };
  "accounts.inference.team": { params: InferenceTeamParams; result: InferenceFlow };
  "accounts.inference.project": { params: InferenceProjectParams; result: InferenceFlow };
  "accounts.inference.create_project": { params: InferenceCreateProjectParams; result: InferenceFlow };
  "accounts.inference.retry": { params: InferenceFlowParams; result: InferenceFlow };
  "accounts.inference.rotate": { params: EmptyParams; result: InferenceFlow };
  "accounts.inference.status": { params: EmptyParams; result: InferenceAccountStatus };
  "accounts.inference.setup": { params: EmptyParams; result: InferenceAccountStatus };
  "accounts.inference.logout": { params: EmptyParams; result: InferenceLogoutResult };
  "accounts.inference.cleanup": { params: EmptyParams; result: InferenceCleanupResult };
  "accounts.inference.retry_cleanup": { params: EmptyParams; result: InferenceCleanupResult };
  "goals.formulate": { params: FormulateGoalParams; result: Admission };
  "goals.formulation": { params: GoalFormulationParams; result: GoalFormulation };
  "goals.create": { params: CreateGoalParams; result: GoalAdmission };
  "goals.current": { params: SessionParams; result: CurrentGoalResult };
  "goals.get": { params: GoalParams; result: Goal };
  "goals.resume": { params: ResumeGoalParams; result: Admission };
  "goals.cancel": { params: GoalParams; result: GoalChange };
  "schedules.create": { params: CreateScheduleParams; result: ScheduleAdmission };
  "schedules.get": { params: ScheduleParams; result: ScheduleResult };
  "schedules.list": { params: ListSchedulesParams; result: SchedulesResult };
  "schedules.cancel": { params: ScheduleParams; result: ScheduleAdmission };
  "sessions.compact": { params: CompactParams; result: Admission };
  "context.head": { params: SessionParams; result: ContextHead };
  "context.compaction": { params: CompactionParams; result: CompactionResult };
  "context.compactions": { params: CompactionsParams; result: CompactionsResult };
  "context.select": { params: SelectCompactionParams; result: ContextHead };
  "context.snapshot": { params: SessionParams; result: HistorySnapshot };
  "context.list": { params: ContextHistoryParams; result: HistoryMetadataResult };
  "context.read": { params: ReadHistoryParams; result: ReadHistoryResult };
  "context.search": { params: SearchHistoryParams; result: SearchHistoryResult };
  "turns.output": { params: TurnParams; result: TurnOutputResult };
  "turns.instructions": { params: TurnParams; result: InstructionManifestResult };
  "skills.list": { params: ListSkillsParams; result: ListSkillsResult };
  "completions.list": { params: ListCompletionsParams; result: ListCompletionsResult };
  "completions.read": { params: ReadCompletionParams; result: ReadCompletionResult };
  "state.subscribe": { params: SubscribeStateParams; result: StateSubscription };
  "state.subscriptions": { params: StateSubscriptionsParams; result: StateSubscriptionsResult };
  "state.unsubscribe": { params: UnsubscribeStateParams; result: StateSubscription };
  "state.get": { params: GetStateParams; result: StateVersion };
  "state.write": { params: WriteStateParams; result: StateVersion };
  "state.append": { params: WriteStateParams; result: StateVersion };
  "state.read": { params: ReadStateParams; result: ReadStateResult };
  "state.list": { params: ListStateParams; result: StateVersionsResult };
  "state.history": { params: StateHistoryParams; result: StateVersionsResult };
  "mail.send": { params: SendMailParams; result: MailAdmission };
  "mail.list": { params: ListMailParams; result: ListMailResult };
  "mail.read": { params: ReadMailParams; result: ReadMailResult };
  "resources.list": { params: SessionParams; result: ResourcesResult };
  "resources.set": { params: SetResourceParams; result: ResourceUsage };
  "budgets.list": { params: SessionParams; result: BudgetsResult };
  "budgets.set": { params: SetBudgetParams; result: Budget };
  "sessions.observe": { params: HistoryParams; result: SessionObservation };
  "cells.get": { params: CellParams; result: Cell };
  "turns.cells": { params: CellsParams; result: CellsResult };
  "grants.create": { params: CreateGrantParams; result: Grant };
  "grants.list": { params: GrantsParams; result: GrantsResult };
  "grants.revoke": { params: GrantParams; result: Grant };
  "operations.get": { params: HostOperationParams; result: HostOperation };
  "turns.operations": { params: HostOperationsParams; result: HostOperationsResult };
  "permissions.list": { params: PermissionsParams; result: PermissionsResult };
  "permissions.resolve": { params: ResolvePermissionParams; result: Permission };
  "permissions.policy": { params: SessionParams; result: PermissionPolicy };
  "permissions.set_mode": { params: SetPermissionModeParams; result: PermissionModeEdit };
  "permissions.mode_edit": { params: PermissionModeEditParams; result: PermissionModeEdit };
  "host.permission_default": { params: EmptyParams; result: DefaultPermissionMode };
  "host.set_permission_default": { params: SetDefaultPermissionModeParams; result: DefaultPermissionMode };
  "questions.get": { params: QuestionParams; result: Question };
  "questions.list": { params: QuestionsParams; result: QuestionsResult };
  "questions.answer": { params: AnswerQuestionParams; result: Question };
  "initialize": { params: InitializeParams; result: InitializeResult };
  "trees.create": { params: CreateTreeParams; result: CreateTreeResult };
  "trees.creation": { params: TreeCreationParams; result: CreateTreeResult };
  "trees.catalog": { params: EmptyParams; result: TreeCatalog };
  "trees.list": { params: ListTreesParams; result: ListTreesResult };
  "definitions.list": { params: ListDefinitionsParams; result: ListDefinitionsResult };
  "trees.get": { params: TreeParams; result: Tree };
  "trees.update": { params: UpdateTreeParams; result: Tree };
  "trees.title_decision": { params: TreeParams; result: AutomaticTitleDecision };
  "trees.title_result": { params: AutomaticTitleResultParams; result: AutomaticTitleResult };
  "sessions.get": { params: SessionParams; result: Session };
  "sessions.spawn": { params: SpawnSessionParams; result: SpawnSessionResult };
  "sessions.list": { params: ListSessionsParams; result: ListSessionsResult };
  "sessions.configure": { params: UpdateConfigurationParams; result: Session };
  "sessions.submit": { params: SubmitParams; result: Admission };
  "sessions.history_page": { params: HistoryPageParams; result: HistoryPageResult };
  "sessions.history": { params: HistoryParams; result: HistoryResult };
  "sessions.rewind": { params: RewindParams; result: HistoryEdit };
  "sessions.fork": { params: ForkParams; result: ForkResult };
  "sessions.lifecycle": { params: LifecycleParams; result: Session };
  "sessions.delete": { params: SessionParams; result: DeleteResult };
  "turns.get": { params: TurnParams; result: Turn };
  "turns.attempts": { params: ModelAttemptsParams; result: ModelAttemptsResult };
  "turns.cancel": { params: TurnParams; result: Turn };
  "sessions.activity": { params: SessionParams; result: SessionActivity };
  "inputs.page": { params: InputPageParams; result: InputPageResult };
  "inputs.get": { params: SessionInputParams; result: Input };
  "inputs.cancel": { params: InputParams; result: Input };
  "receipts.match": { params: MatchReceiptParams; result: Admission };
  "receipts.get": { params: RequestIdentity; result: Admission };
  "content.put": { params: PutContentParams; result: ContentReference };
  "content.read": { params: ReadContentParams; result: ReadContentResult };
  "definitions.register": { params: DefinitionDocument; result: Definition };
  "definitions.get": { params: DefinitionRef; result: Definition };
}
export declare const manifest: { major: 4; minor: number; operations: readonly { name: keyof Operations; params: keyof ContractTypes; result: keyof ContractTypes }[] };
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];
