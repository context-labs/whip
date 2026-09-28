package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

type Operation struct {
	Name           string
	Params, Result reflect.Type
}

func Operations() []Operation {
	return []Operation{
		{"accounts.openai.begin", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.get", reflect.TypeFor[OpenAIFlowParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.list", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIFlowsResult]()},
		{"accounts.openai.cancel", reflect.TypeFor[OpenAIFlowParams](), reflect.TypeFor[OpenAILoginFlow]()},
		{"accounts.openai.status", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},
		{"accounts.openai.setup", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},
		{"accounts.openai.logout", reflect.TypeFor[EmptyParams](), reflect.TypeFor[OpenAIAccountStatus]()},

		{"goals.formulate", reflect.TypeFor[FormulateGoalParams](), reflect.TypeFor[Admission]()},
		{"goals.formulation", reflect.TypeFor[GoalFormulationParams](), reflect.TypeFor[GoalFormulation]()},
		{"goals.create", reflect.TypeFor[CreateGoalParams](), reflect.TypeFor[GoalAdmission]()},
		{"goals.current", reflect.TypeFor[SessionParams](), reflect.TypeFor[CurrentGoalResult]()},
		{"goals.get", reflect.TypeFor[GoalParams](), reflect.TypeFor[Goal]()},
		{"goals.resume", reflect.TypeFor[ResumeGoalParams](), reflect.TypeFor[Admission]()},
		{"goals.cancel", reflect.TypeFor[GoalParams](), reflect.TypeFor[GoalChange]()},
		{"schedules.create", reflect.TypeFor[CreateScheduleParams](), reflect.TypeFor[ScheduleAdmission]()},
		{"schedules.get", reflect.TypeFor[ScheduleParams](), reflect.TypeFor[ScheduleResult]()},
		{"schedules.list", reflect.TypeFor[ListSchedulesParams](), reflect.TypeFor[SchedulesResult]()},
		{"schedules.cancel", reflect.TypeFor[ScheduleParams](), reflect.TypeFor[ScheduleAdmission]()},
		{"sessions.compact", reflect.TypeFor[CompactParams](), reflect.TypeFor[Admission]()},
		{"context.head", reflect.TypeFor[SessionParams](), reflect.TypeFor[ContextHead]()},
		{"context.compaction", reflect.TypeFor[CompactionParams](), reflect.TypeFor[CompactionResult]()},
		{"context.compactions", reflect.TypeFor[CompactionsParams](), reflect.TypeFor[CompactionsResult]()},
		{"context.select", reflect.TypeFor[SelectCompactionParams](), reflect.TypeFor[ContextHead]()},
		{"context.snapshot", reflect.TypeFor[SessionParams](), reflect.TypeFor[HistorySnapshot]()},
		{"context.list", reflect.TypeFor[ContextHistoryParams](), reflect.TypeFor[HistoryMetadataResult]()},
		{"context.read", reflect.TypeFor[ReadHistoryParams](), reflect.TypeFor[ReadHistoryResult]()},
		{"context.search", reflect.TypeFor[SearchHistoryParams](), reflect.TypeFor[SearchHistoryResult]()},
		{"turns.output", reflect.TypeFor[TurnParams](), reflect.TypeFor[TurnOutputResult]()},
		{"turns.instructions", reflect.TypeFor[TurnParams](), reflect.TypeFor[InstructionManifestResult]()},
		{"skills.list", reflect.TypeFor[ListSkillsParams](), reflect.TypeFor[ListSkillsResult]()},
		{"completions.list", reflect.TypeFor[ListCompletionsParams](), reflect.TypeFor[ListCompletionsResult]()},
		{"completions.read", reflect.TypeFor[ReadCompletionParams](), reflect.TypeFor[ReadCompletionResult]()},
		{"state.subscribe", reflect.TypeFor[SubscribeStateParams](), reflect.TypeFor[StateSubscription]()},
		{"state.subscriptions", reflect.TypeFor[StateSubscriptionsParams](), reflect.TypeFor[StateSubscriptionsResult]()},
		{"state.unsubscribe", reflect.TypeFor[UnsubscribeStateParams](), reflect.TypeFor[StateSubscription]()},
		{"state.get", reflect.TypeFor[GetStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.write", reflect.TypeFor[WriteStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.append", reflect.TypeFor[WriteStateParams](), reflect.TypeFor[StateVersion]()},
		{"state.read", reflect.TypeFor[ReadStateParams](), reflect.TypeFor[ReadStateResult]()},
		{"state.list", reflect.TypeFor[ListStateParams](), reflect.TypeFor[StateVersionsResult]()},
		{"state.history", reflect.TypeFor[StateHistoryParams](), reflect.TypeFor[StateVersionsResult]()},
		{"mail.send", reflect.TypeFor[SendMailParams](), reflect.TypeFor[MailAdmission]()},
		{"mail.list", reflect.TypeFor[ListMailParams](), reflect.TypeFor[ListMailResult]()},
		{"mail.read", reflect.TypeFor[ReadMailParams](), reflect.TypeFor[ReadMailResult]()},
		{"resources.list", reflect.TypeFor[SessionParams](), reflect.TypeFor[ResourcesResult]()},
		{"resources.set", reflect.TypeFor[SetResourceParams](), reflect.TypeFor[ResourceUsage]()},
		{"budgets.list", reflect.TypeFor[SessionParams](), reflect.TypeFor[BudgetsResult]()},
		{"budgets.set", reflect.TypeFor[SetBudgetParams](), reflect.TypeFor[Budget]()},
		{"sessions.observe", reflect.TypeFor[HistoryParams](), reflect.TypeFor[SessionObservation]()},
		{"cells.get", reflect.TypeFor[CellParams](), reflect.TypeFor[Cell]()},
		{"turns.cells", reflect.TypeFor[CellsParams](), reflect.TypeFor[CellsResult]()},
		{"grants.create", reflect.TypeFor[CreateGrantParams](), reflect.TypeFor[Grant]()},
		{"grants.list", reflect.TypeFor[GrantsParams](), reflect.TypeFor[GrantsResult]()},
		{"grants.revoke", reflect.TypeFor[GrantParams](), reflect.TypeFor[Grant]()},
		{"operations.get", reflect.TypeFor[HostOperationParams](), reflect.TypeFor[HostOperation]()},
		{"turns.operations", reflect.TypeFor[HostOperationsParams](), reflect.TypeFor[HostOperationsResult]()},
		{"permissions.list", reflect.TypeFor[PermissionsParams](), reflect.TypeFor[PermissionsResult]()},
		{"permissions.resolve", reflect.TypeFor[ResolvePermissionParams](), reflect.TypeFor[Permission]()},
		{"initialize", reflect.TypeFor[InitializeParams](), reflect.TypeFor[InitializeResult]()},
		{"trees.create", reflect.TypeFor[CreateTreeParams](), reflect.TypeFor[CreateTreeResult]()},
		{"trees.get", reflect.TypeFor[TreeParams](), reflect.TypeFor[Tree]()},
		{"trees.update", reflect.TypeFor[UpdateTreeParams](), reflect.TypeFor[Tree]()},
		{"sessions.get", reflect.TypeFor[SessionParams](), reflect.TypeFor[Session]()},
		{"sessions.spawn", reflect.TypeFor[SpawnSessionParams](), reflect.TypeFor[SpawnSessionResult]()},
		{"sessions.list", reflect.TypeFor[ListSessionsParams](), reflect.TypeFor[ListSessionsResult]()},
		{"sessions.configure", reflect.TypeFor[UpdateConfigurationParams](), reflect.TypeFor[Session]()},
		{"sessions.submit", reflect.TypeFor[SubmitParams](), reflect.TypeFor[Admission]()},
		{"sessions.history", reflect.TypeFor[HistoryParams](), reflect.TypeFor[HistoryResult]()},
		{"sessions.lifecycle", reflect.TypeFor[LifecycleParams](), reflect.TypeFor[Session]()},
		{"sessions.delete", reflect.TypeFor[SessionParams](), reflect.TypeFor[DeleteResult]()},
		{"turns.get", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"turns.attempts", reflect.TypeFor[ModelAttemptsParams](), reflect.TypeFor[ModelAttemptsResult]()},
		{"turns.cancel", reflect.TypeFor[TurnParams](), reflect.TypeFor[Turn]()},
		{"inputs.cancel", reflect.TypeFor[InputParams](), reflect.TypeFor[Input]()},
		{"receipts.get", reflect.TypeFor[RequestIdentity](), reflect.TypeFor[Admission]()},
		{"content.put", reflect.TypeFor[PutContentParams](), reflect.TypeFor[ContentReference]()},
		{"content.read", reflect.TypeFor[ReadContentParams](), reflect.TypeFor[ReadContentResult]()},
		{"definitions.register", reflect.TypeFor[DefinitionDocument](), reflect.TypeFor[Definition]()},
		{"definitions.get", reflect.TypeFor[DefinitionRef](), reflect.TypeFor[Definition]()},
	}
}

func Types() map[string]reflect.Type {
	result := map[string]reflect.Type{}
	result["RPCError"] = reflect.TypeFor[RPCError]()
	result["Request"] = reflect.TypeFor[Request]()
	result["Response"] = reflect.TypeFor[Response]()
	result["Part"] = reflect.TypeFor[Part]()
	result["Message"] = reflect.TypeFor[Message]()
	result["ToolCall"] = reflect.TypeFor[ToolCall]()
	result["ToolResult"] = reflect.TypeFor[ToolResult]()
	for _, op := range Operations() {
		result[op.Params.Name()] = op.Params
		result[op.Result.Name()] = op.Result
	}
	return result
}

func SchemaFor(t reflect.Type) (*jsonschema.Schema, error) {
	schema, err := jsonschema.ForType(t, &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[AccountTimestamp](): {Type: "string", Format: "account-time"},
		reflect.TypeFor[ID]():               {Type: "string", Pattern: `^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`},
		reflect.TypeFor[Counter]():          {Type: "string", Pattern: `^(0|[1-9][0-9]{0,18})$`, Format: "counter"},
		reflect.TypeFor[json.RawMessage]():  {},
		reflect.TypeFor[Part]():             partSchema("text", "content", "tool_call", "tool_result"),
		reflect.TypeFor[ToolCall]():         toolCallSchema(),
		reflect.TypeFor[ToolResult]():       toolResultSchema(),
	}})
	if err != nil {
		return nil, err
	}
	applyTags(schema, t)
	if t == reflect.TypeFor[Response]() {
		schema.OneOf = []*jsonschema.Schema{{Required: []string{"result"}, Not: &jsonschema.Schema{Required: []string{"error"}}}, {Required: []string{"error"}, Not: &jsonschema.Schema{Required: []string{"result"}}}}
	}
	schema.Schema = "http://json-schema.org/draft-07/schema#"
	schema.ID = "https://whip.dev/protocol/v4/" + t.Name()
	schema.Title = t.Name()
	return schema, nil
}

func applyTags(schema *jsonschema.Schema, t reflect.Type) {
	if schema == nil {
		return
	}
	nullable := t.Kind() == reflect.Pointer
	if nullable {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		for field := range t.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			child := schema.Properties[name]
			if child == nil {
				continue
			}
			if value := field.Tag.Get("enum"); value != "" {
				for item := range strings.SplitSeq(value, ",") {
					child.Enum = append(child.Enum, item)
				}
			}
			if value := field.Tag.Get("pattern"); value != "" {
				child.Pattern = value
			}
			if value := field.Tag.Get("min"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Minimum = &number
			}
			if value := field.Tag.Get("max"); value != "" {
				number, _ := strconv.ParseFloat(value, 64)
				child.Maximum = &number
			}
			applyTags(child, field.Type)
			if field.Type == reflect.TypeFor[[]Part]() {
				child.Type = "array"
				child.Types = nil
				child.MinItems = new(1)
				child.MaxItems = new(128)
			}
		}
		accountSchema(schema, t)
		if t == reflect.TypeFor[GoalFormulationRequest]() {
			schema.Properties["tail_messages"] = &jsonschema.Schema{OneOf: []*jsonschema.Schema{
				{Type: "integer", Enum: []any{0}},
				{Type: "integer", Minimum: new(2.0), Maximum: new(100.0)},
			}}
		}
		if t == reflect.TypeFor[Configuration]() {
			schema.Properties["compaction"].Properties["threshold_percent"].Minimum = new(1.0)
		}
		if t == reflect.TypeFor[ListSkillsResult]() {
			schema.Properties["items"].Type = "array"
			schema.Properties["items"].Types = nil
			schema.Properties["items"].MaxItems = new(100)
		}
		if t == reflect.TypeFor[InstructionManifest]() {
			schema.Properties["sources"].Type = "array"
			schema.Properties["sources"].Types = nil
			schema.Properties["sources"].MaxItems = new(1152)
		}
		if t == reflect.TypeFor[Input]() || t == reflect.TypeFor[SubmitParams]() || t == reflect.TypeFor[SpawnSessionParams]() {
			schema.Properties["parts"].Items = partSchema("text", "content")
		}
		if t == reflect.TypeFor[Input]() {
			prompt := schema.CloneSchemas()
			prompt.Type, prompt.Types = "object", nil
			prompt.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{"prompt"}}
			compact := schema.CloneSchemas()
			compact.Type, compact.Types = "object", nil
			compact.Properties["kind"] = &jsonschema.Schema{Type: "string", Enum: []any{"compact", "goal_formulation"}}
			compact.Properties["parts"] = &jsonschema.Schema{Type: "array", MaxItems: new(0), Items: partSchema("text", "content")}
			*schema = jsonschema.Schema{OneOf: []*jsonschema.Schema{prompt, compact}}
			if nullable {
				schema.OneOf = append(schema.OneOf, &jsonschema.Schema{Type: "null"})
			}
		}
		if t == reflect.TypeFor[Message]() {
			var variants []*jsonschema.Schema
			for _, role := range []string{"user", "system", "assistant", "tool"} {
				items := partSchema("text", "content")
				maxItems := 128
				switch role {
				case "assistant":
					items = partSchema("text", "content", "tool_call")
				case "tool":
					items, maxItems = partSchema("tool_result"), 1
				}
				// Keep common required fields in each variant. Conditional-only
				// branches validate in JSON Schema but lose those fields in TS unions.
				variant := schema.CloneSchemas()
				variant.Properties["role"] = &jsonschema.Schema{Type: "string", Enum: []any{role}}
				variant.Properties["parts"] = &jsonschema.Schema{Type: "array", MinItems: new(1), MaxItems: &maxItems, Items: items}
				variants = append(variants, variant)
			}
			*schema = jsonschema.Schema{OneOf: variants}
		}
	case reflect.Slice:
		if t != reflect.TypeFor[json.RawMessage]() && schema.Type == "array" {
			schema.Type = ""
			schema.Types = []string{"array", "null"}
		}
		applyTags(schema.Items, t.Elem())
	case reflect.Map:
		if schema.Type == "object" {
			schema.Type = ""
			schema.Types = []string{"object", "null"}
		}
		applyTags(schema.AdditionalProperties, t.Elem())
	}
}

// Validate checks schema shape and Go decoding together. Shape validation owns
// required/unknown fields; the typed decoder enforces exact decimal bounds.
func Validate(name string, raw []byte) error {
	t, ok := Types()[name]
	if !ok {
		return fmt.Errorf("unknown contract type %q", name)
	}
	if len(raw) > MaxFrameBytes {
		return errors.New("contract document exceeds 8 MiB")
	}
	schema, err := SchemaFor(t)
	if err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if err := resolved.Validate(value); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(reflect.New(t).Interface()); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
