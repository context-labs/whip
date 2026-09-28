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
export interface CreateTreeParams {
  metadata: {
    title: null | string;
    archived: boolean;
    pinned: boolean;
  };
  engine: "starlark" | "quickjs";
  policy: {
    max_depth: number;
    max_sessions: number;
    max_queued_inputs_per_session: number;
  };
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
    policy: {
      max_depth: number;
      max_sessions: number;
      max_queued_inputs_per_session: number;
    };
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
export interface HistoryParams {
  session_id: string;
  after: string;
  limit: number;
}
export interface HistoryResult {
  items:
    | null
    | {
        id: string;
        session_id: string;
        turn_id: string;
        input_id: null | string;
        sequence: string;
        role: "system" | "user" | "assistant" | "tool";
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
export interface RPCError {
  code: number;
  message: string;
  kind:
    "INVALID" | "NOT_FOUND" | "CONFLICT" | "BUSY" | "LIMIT" | "STOPPED" | "CLOSED" | "IDENTITY" | "METHOD" | "INTERNAL";
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
export interface SessionParams {
  session_id: string;
}
export interface SpawnSessionParams {
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
export interface Tree {
  id: string;
  metadata: {
    title: null | string;
    archived: boolean;
    pinned: boolean;
  };
  engine: "starlark" | "quickjs";
  policy: {
    max_depth: number;
    max_sessions: number;
    max_queued_inputs_per_session: number;
  };
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

export interface ContractTypes {
  Admission: Admission;
  CreateTreeParams: CreateTreeParams;
  CreateTreeResult: CreateTreeResult;
  Definition: Definition;
  DefinitionDocument: DefinitionDocument;
  DefinitionRef: DefinitionRef;
  DeleteResult: DeleteResult;
  HistoryParams: HistoryParams;
  HistoryResult: HistoryResult;
  InitializeParams: InitializeParams;
  InitializeResult: InitializeResult;
  Input: Input;
  InputParams: InputParams;
  LifecycleParams: LifecycleParams;
  ListSessionsParams: ListSessionsParams;
  ListSessionsResult: ListSessionsResult;
  RPCError: RPCError;
  Request: Request;
  RequestIdentity: RequestIdentity;
  Response: Response;
  Session: Session;
  SessionParams: SessionParams;
  SpawnSessionParams: SpawnSessionParams;
  SubmitParams: SubmitParams;
  Tree: Tree;
  TreeParams: TreeParams;
  Turn: Turn;
  TurnParams: TurnParams;
  UpdateConfigurationParams: UpdateConfigurationParams;
  UpdateTreeParams: UpdateTreeParams;
}
export interface Operations {
  "initialize": { params: InitializeParams; result: InitializeResult };
  "trees.create": { params: CreateTreeParams; result: CreateTreeResult };
  "trees.get": { params: TreeParams; result: Tree };
  "trees.update": { params: UpdateTreeParams; result: Tree };
  "sessions.get": { params: SessionParams; result: Session };
  "sessions.spawn": { params: SpawnSessionParams; result: Session };
  "sessions.list": { params: ListSessionsParams; result: ListSessionsResult };
  "sessions.configure": { params: UpdateConfigurationParams; result: Session };
  "sessions.submit": { params: SubmitParams; result: Admission };
  "sessions.history": { params: HistoryParams; result: HistoryResult };
  "sessions.lifecycle": { params: LifecycleParams; result: Session };
  "sessions.delete": { params: SessionParams; result: DeleteResult };
  "turns.get": { params: TurnParams; result: Turn };
  "turns.cancel": { params: TurnParams; result: Turn };
  "inputs.cancel": { params: InputParams; result: Input };
  "receipts.get": { params: RequestIdentity; result: Admission };
  "definitions.register": { params: DefinitionDocument; result: Definition };
  "definitions.get": { params: DefinitionRef; result: Definition };
}
export declare const manifest: { major: 4; minor: number; operations: readonly { name: keyof Operations; params: keyof ContractTypes; result: keyof ContractTypes }[] };
export declare function validate<T extends keyof ContractTypes>(type: T, value: unknown): value is ContractTypes[T];
export declare function assertValid<T extends keyof ContractTypes>(type: T, value: unknown): asserts value is ContractTypes[T];
