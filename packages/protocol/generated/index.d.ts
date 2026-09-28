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
  input: null | {
    id: string;
    session_id: string;
    source: "user" | "agent" | "schedule";
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
  };
  turn: null | {
    id: string;
    session_id: string;
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
export interface ContentReference {
  id: string;
  session_id: string;
  digest: string;
  size: string;
  media_type: string;
  created_at: string;
}
export interface CreateGrantParams {
  id: string;
  session_id: string;
  capability: string;
  resource: string;
  issuer_id?: null | string;
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
          "depth" | "descendants" | "queued_inputs" | "active_operations" | "subscriptions" | "runnable_descendants";
        limit: null | string;
      }[];
  definition: {
    id: string;
    revision: string;
  };
  overrides: {
    model?: null | {
      provider: string;
      name: string;
      effort: string;
    };
    instructions?: null | {
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
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
    id: string;
    tree_id: string;
    parent_id: null | string;
    definition: {
      id: string;
      revision: string;
    };
    config_revision: string;
    configuration: {
      model: {
        provider: string;
        name: string;
        effort: string;
      };
      instructions: {
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
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
export interface Definition {
  ref: {
    id: string;
    revision: string;
  };
  document: {
    id: string;
    name: string;
    defaults: {
      model?: null | {
        provider: string;
        name: string;
        effort: string;
      };
      instructions?: null | {
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
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
    model?: null | {
      provider: string;
      name: string;
      effort: string;
    };
    instructions?: null | {
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
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
export interface GetStateParams {
  session_id: string;
  scope: "session" | "tree";
  key: string;
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
export interface HistoryParams {
  session_id: string;
  after: string;
  limit: number;
}
export interface HistoryResult {
  items:
    | null
    | (
        | {
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
export interface Input {
  id: string;
  session_id: string;
  source: "user" | "agent" | "schedule";
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
}
export interface InputParams {
  input_id: string;
}
export interface LifecycleParams {
  session_id: string;
  lifecycle: "active" | "stopped";
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
          kind: "session" | "state";
          id: string;
        };
        recipient_id: string;
        delivery: "queued" | "steer" | "next_turn";
        subject: string;
        body_bytes: string;
        state: "pending" | "delivered" | "done";
        available_at: string;
        created_at: string;
        revised_at: string;
      }[];
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
        id: string;
        tree_id: string;
        parent_id: null | string;
        definition: {
          id: string;
          revision: string;
        };
        config_revision: string;
        configuration: {
          model: {
            provider: string;
            name: string;
            effort: string;
          };
          instructions: {
            text: string;
            project_files: null | string[];
            discover_skills: boolean;
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
      kind: "session" | "state";
      id: string;
    };
    recipient_id: string;
    delivery: "queued" | "steer" | "next_turn";
    subject: string;
    body_bytes: string;
    state: "pending" | "delivered" | "done";
    available_at: string;
    created_at: string;
    revised_at: string;
  };
  deleted_at: null | string;
}
export type Message =
  | {
      id: string;
      session_id: string;
      turn_id: string;
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
      id: string;
      session_id: string;
      turn_id: string;
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
      id: string;
      session_id: string;
      turn_id: string;
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
      id: string;
      session_id: string;
      turn_id: string;
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
        request: {
          purpose: string;
          model: {
            provider: string;
            name: string;
            effort: string;
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
    "INVALID" | "NOT_FOUND" | "CONFLICT" | "BUSY" | "LIMIT" | "STOPPED" | "CLOSED" | "IDENTITY" | "METHOD" | "INTERNAL";
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
export interface ReadMailParams {
  session_id: string;
  mail_id: string;
}
export interface ReadMailResult {
  mail: {
    id: string;
    revision: string;
    source: {
      kind: "session" | "state";
      id: string;
    };
    recipient_id: string;
    delivery: "queued" | "steer" | "next_turn";
    subject: string;
    body_bytes: string;
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
  kind: "depth" | "descendants" | "queued_inputs" | "active_operations" | "subscriptions" | "runnable_descendants";
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
          "depth" | "descendants" | "queued_inputs" | "active_operations" | "subscriptions" | "runnable_descendants";
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
      | "INTERNAL";
  };
} & {
  [k: string]: unknown;
};
export interface SendMailParams {
  mail_id: string;
  sender_id: string;
  recipient_id: string;
  delivery: "queued" | "steer" | "next_turn";
  subject: string;
  body: string;
  available_at?: null | string;
}
export interface Session {
  id: string;
  tree_id: string;
  parent_id: null | string;
  definition: {
    id: string;
    revision: string;
  };
  config_revision: string;
  configuration: {
    model: {
      provider: string;
      name: string;
      effort: string;
    };
    instructions: {
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
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
  epoch: string;
  messages:
    | null
    | (
        | {
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
            id: string;
            session_id: string;
            turn_id: string;
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
    kind: "depth" | "descendants" | "queued_inputs" | "active_operations" | "subscriptions" | "runnable_descendants";
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
    model?: null | {
      provider: string;
      name: string;
      effort: string;
    };
    instructions?: null | {
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
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
          "depth" | "descendants" | "queued_inputs" | "active_operations" | "subscriptions" | "runnable_descendants";
        limit: null | string;
      }[];
}
export interface SpawnSessionResult {
  session: null | {
    id: string;
    tree_id: string;
    parent_id: null | string;
    definition: {
      id: string;
      revision: string;
    };
    config_revision: string;
    configuration: {
      model: {
        provider: string;
        name: string;
        effort: string;
      };
      instructions: {
        text: string;
        project_files: null | string[];
        discover_skills: boolean;
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
    input: null | {
      id: string;
      session_id: string;
      source: "user" | "agent" | "schedule";
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
    };
    turn: null | {
      id: string;
      session_id: string;
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
  source: "user" | "agent" | "schedule";
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
  id: string;
  session_id: string;
  config_revision: string;
  state: "running" | "cancelling" | "succeeded" | "failed" | "cancelled" | "interrupted";
  failure: null | string;
  started_at: string;
  finished_at: null | string;
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
    model?: null | {
      provider: string;
      name: string;
      effort: string;
    };
    instructions?: null | {
      text: string;
      project_files: null | string[];
      discover_skills: boolean;
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
  ContentReference: ContentReference;
  CreateGrantParams: CreateGrantParams;
  CreateTreeParams: CreateTreeParams;
  CreateTreeResult: CreateTreeResult;
  Definition: Definition;
  DefinitionDocument: DefinitionDocument;
  DefinitionRef: DefinitionRef;
  DeleteResult: DeleteResult;
  GetStateParams: GetStateParams;
  Grant: Grant;
  GrantParams: GrantParams;
  GrantsParams: GrantsParams;
  GrantsResult: GrantsResult;
  HistoryParams: HistoryParams;
  HistoryResult: HistoryResult;
  HostOperation: HostOperation;
  HostOperationParams: HostOperationParams;
  HostOperationsParams: HostOperationsParams;
  HostOperationsResult: HostOperationsResult;
  InitializeParams: InitializeParams;
  InitializeResult: InitializeResult;
  Input: Input;
  InputParams: InputParams;
  LifecycleParams: LifecycleParams;
  ListMailParams: ListMailParams;
  ListMailResult: ListMailResult;
  ListSessionsParams: ListSessionsParams;
  ListSessionsResult: ListSessionsResult;
  ListStateParams: ListStateParams;
  MailAdmission: MailAdmission;
  Message: Message;
  ModelAttemptsParams: ModelAttemptsParams;
  ModelAttemptsResult: ModelAttemptsResult;
  Part: Part;
  Permission: Permission;
  PermissionsParams: PermissionsParams;
  PermissionsResult: PermissionsResult;
  PutContentParams: PutContentParams;
  RPCError: RPCError;
  ReadContentParams: ReadContentParams;
  ReadContentResult: ReadContentResult;
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
  TurnParams: TurnParams;
  UnsubscribeStateParams: UnsubscribeStateParams;
  UpdateConfigurationParams: UpdateConfigurationParams;
  UpdateTreeParams: UpdateTreeParams;
  WriteStateParams: WriteStateParams;
}
export interface Operations {
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
