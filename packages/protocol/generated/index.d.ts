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
        goal: null | {
          id: string;
          revision: string;
        };
        id: string;
        session_id: string;
        source: "user" | "agent" | "schedule" | "goal";
        kind: "compact" | "goal_formulation";
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
    kind: "prompt" | "compact" | "goal_formulation";
    config_revision: string;
    state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
    failure: null | string;
    started_at: string;
    finished_at: null | string;
  };
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
  root: {
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
      model: {
        provider: string;
        name: string;
        effort: string;
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
export interface Definition {
  ref: {
    id: string;
    revision: string;
  };
  document: {
    id: string;
    name: string;
    defaults: {
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
      model: {
        provider: string;
        name: string;
        effort: string;
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
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "compact" | "goal_formulation";
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
      kind: "prompt" | "compact" | "goal_formulation";
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
             * @maxItems 1
             */
            parts: [
              {
                result: {
                  call_id: string;
                  is_error: boolean;
                  output: string;
                };
                type: "tool_result";
              }
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
export interface HostOperation {
  id: string;
  session_id: string;
  turn_id: string;
  cell_id: string;
  request_id: string;
  capability: string;
  resource: string;
  arguments: unknown;
  state: "waiting" | "ready" | "dispatched" | "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
  grant_id: null | string;
  result: null | {
    state: "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
    value?: unknown;
    failure?: null | string;
  };
  created_at: string;
  dispatched_at: null | string;
  finished_at: null | string;
}
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
    | {
        id: string;
        session_id: string;
        turn_id: string;
        cell_id: string;
        request_id: string;
        capability: string;
        resource: string;
        arguments: unknown;
        state: "waiting" | "ready" | "dispatched" | "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
        grant_id: null | string;
        result: null | {
          state: "succeeded" | "failed" | "denied" | "cancelled" | "uncertain";
          value?: unknown;
          failure?: null | string;
        };
        created_at: string;
        dispatched_at: null | string;
        finished_at: null | string;
      }[];
}
export interface InitializeParams {
  major: number;
  expected_runtime_id?: null | string;
}
export interface InitializeResult {
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
      goal: null | {
        id: string;
        revision: string;
      };
      id: string;
      session_id: string;
      source: "user" | "agent" | "schedule" | "goal";
      kind: "compact" | "goal_formulation";
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
          model: {
            provider: string;
            name: string;
            effort: string;
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
       * @maxItems 1
       */
      parts: [
        {
          result: {
            call_id: string;
            is_error: boolean;
            output: string;
          };
          type: "tool_result";
        }
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
          purpose: string;
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
export interface PutContentParams {
  session_id: string;
  reference_id: string;
  media_type: string;
  data_base64: string;
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
    | "ACCOUNT_CREDENTIALS"
    | "ACCOUNT_SETUP"
    | "ACCOUNT_CONFIGURATION"
    | "ACCOUNT_LOGOUT"
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
      | "ACCOUNT_CREDENTIALS"
      | "ACCOUNT_SETUP"
      | "ACCOUNT_CONFIGURATION"
      | "ACCOUNT_LOGOUT"
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
    model: {
      provider: string;
      name: string;
      effort: string;
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
             * @maxItems 1
             */
            parts: [
              {
                result: {
                  call_id: string;
                  is_error: boolean;
                  output: string;
                };
                type: "tool_result";
              }
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
      model: {
        provider: string;
        name: string;
        effort: string;
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
          goal: null | {
            id: string;
            revision: string;
          };
          id: string;
          session_id: string;
          source: "user" | "agent" | "schedule" | "goal";
          kind: "compact" | "goal_formulation";
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
      kind: "prompt" | "compact" | "goal_formulation";
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
  kind: "prompt" | "compact" | "goal_formulation";
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
  Budget: Budget;
  BudgetsResult: BudgetsResult;
  Cell: Cell;
  CellParams: CellParams;
  CellsParams: CellsParams;
  CellsResult: CellsResult;
  CompactParams: CompactParams;
  CompactionParams: CompactionParams;
  CompactionResult: CompactionResult;
  CompactionsParams: CompactionsParams;
  CompactionsResult: CompactionsResult;
  ContentReference: ContentReference;
  ContextHead: ContextHead;
  ContextHistoryParams: ContextHistoryParams;
  CreateGoalParams: CreateGoalParams;
  CreateGrantParams: CreateGrantParams;
  CreateScheduleParams: CreateScheduleParams;
  CreateTreeParams: CreateTreeParams;
  CreateTreeResult: CreateTreeResult;
  CurrentGoalResult: CurrentGoalResult;
  Definition: Definition;
  DefinitionDocument: DefinitionDocument;
  DefinitionRef: DefinitionRef;
  DeleteResult: DeleteResult;
  EmptyParams: EmptyParams;
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
  HistoryParams: HistoryParams;
  HistoryResult: HistoryResult;
  HistorySnapshot: HistorySnapshot;
  HostOperation: HostOperation;
  HostOperationParams: HostOperationParams;
  HostOperationsParams: HostOperationsParams;
  HostOperationsResult: HostOperationsResult;
  InitializeParams: InitializeParams;
  InitializeResult: InitializeResult;
  Input: Input;
  InputParams: InputParams;
  InstructionManifestResult: InstructionManifestResult;
  LifecycleParams: LifecycleParams;
  ListCompletionsParams: ListCompletionsParams;
  ListCompletionsResult: ListCompletionsResult;
  ListMailParams: ListMailParams;
  ListMailResult: ListMailResult;
  ListSchedulesParams: ListSchedulesParams;
  ListSessionsParams: ListSessionsParams;
  ListSessionsResult: ListSessionsResult;
  ListSkillsParams: ListSkillsParams;
  ListSkillsResult: ListSkillsResult;
  ListStateParams: ListStateParams;
  MailAdmission: MailAdmission;
  Message: Message;
  ModelAttemptsParams: ModelAttemptsParams;
  ModelAttemptsResult: ModelAttemptsResult;
  OpenAIAccountStatus: OpenAIAccountStatus;
  OpenAIFlowParams: OpenAIFlowParams;
  OpenAIFlowsResult: OpenAIFlowsResult;
  OpenAILoginFlow: OpenAILoginFlow;
  Part: Part;
  Permission: Permission;
  PermissionsParams: PermissionsParams;
  PermissionsResult: PermissionsResult;
  PutContentParams: PutContentParams;
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
  Request: Request;
  RequestIdentity: RequestIdentity;
  ResolvePermissionParams: ResolvePermissionParams;
  ResourceUsage: ResourceUsage;
  ResourcesResult: ResourcesResult;
  Response: Response;
  ResumeGoalParams: ResumeGoalParams;
  RewindParams: RewindParams;
  ScheduleAdmission: ScheduleAdmission;
  ScheduleParams: ScheduleParams;
  ScheduleResult: ScheduleResult;
  SchedulesResult: SchedulesResult;
  SearchHistoryParams: SearchHistoryParams;
  SearchHistoryResult: SearchHistoryResult;
  SelectCompactionParams: SelectCompactionParams;
  SendMailParams: SendMailParams;
  Session: Session;
  SessionObservation: SessionObservation;
  SessionParams: SessionParams;
  SetBudgetParams: SetBudgetParams;
  SetResourceParams: SetResourceParams;
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
  ToolCall: ToolCall;
  ToolResult: ToolResult;
  Tree: Tree;
  TreeParams: TreeParams;
  Turn: Turn;
  TurnOutputResult: TurnOutputResult;
  TurnParams: TurnParams;
  UnsubscribeStateParams: UnsubscribeStateParams;
  UpdateConfigurationParams: UpdateConfigurationParams;
  UpdateTreeParams: UpdateTreeParams;
  WriteStateParams: WriteStateParams;
}
export interface Operations {
  "accounts.openai.begin": { params: EmptyParams; result: OpenAILoginFlow };
  "accounts.openai.get": { params: OpenAIFlowParams; result: OpenAILoginFlow };
  "accounts.openai.list": { params: EmptyParams; result: OpenAIFlowsResult };
  "accounts.openai.cancel": { params: OpenAIFlowParams; result: OpenAILoginFlow };
  "accounts.openai.status": { params: EmptyParams; result: OpenAIAccountStatus };
  "accounts.openai.setup": { params: EmptyParams; result: OpenAIAccountStatus };
  "accounts.openai.logout": { params: EmptyParams; result: OpenAIAccountStatus };
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
  "initialize": { params: InitializeParams; result: InitializeResult };
  "trees.create": { params: CreateTreeParams; result: CreateTreeResult };
  "trees.get": { params: TreeParams; result: Tree };
  "trees.update": { params: UpdateTreeParams; result: Tree };
  "sessions.get": { params: SessionParams; result: Session };
  "sessions.spawn": { params: SpawnSessionParams; result: SpawnSessionResult };
  "sessions.list": { params: ListSessionsParams; result: ListSessionsResult };
  "sessions.configure": { params: UpdateConfigurationParams; result: Session };
  "sessions.submit": { params: SubmitParams; result: Admission };
  "sessions.history": { params: HistoryParams; result: HistoryResult };
  "sessions.rewind": { params: RewindParams; result: HistoryEdit };
  "sessions.fork": { params: ForkParams; result: ForkResult };
  "sessions.lifecycle": { params: LifecycleParams; result: Session };
  "sessions.delete": { params: SessionParams; result: DeleteResult };
  "turns.get": { params: TurnParams; result: Turn };
  "turns.attempts": { params: ModelAttemptsParams; result: ModelAttemptsResult };
  "turns.cancel": { params: TurnParams; result: Turn };
  "inputs.cancel": { params: InputParams; result: Input };
  "receipts.get": { params: RequestIdentity; result: Admission };
  "content.put": { params: PutContentParams; result: ContentReference };
  "content.read": { params: ReadContentParams; result: ReadContentResult };
  "definitions.register": { params: DefinitionDocument; result: Definition };
  "definitions.get": { params: DefinitionRef; result: Definition };
}
export declare const manifest: { major: 4; minor: number; operations: readonly { name: keyof Operations; params: keyof ContractTypes; result: keyof ContractTypes }[] };
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];
