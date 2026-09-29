package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type TracePageParams struct {
	RootID ID       `json:"root_id"`
	After  *Counter `json:"after,omitempty"`
	// Before is absent for forward reads; present null opens the newest page.
	Before           **Counter `json:"before,omitempty"`
	ExpectedRevision *Counter  `json:"expected_revision"`
	TraceID          string    `json:"trace_id" pattern:"^([a-f0-9]{32})?$"`
	RootsOnly        bool      `json:"roots_only"`
	Limit            int       `json:"limit" min:"1" max:"2048"`
	MaxBytes         int       `json:"max_bytes" min:"4096" max:"524288"`
}
type TraceAttribute struct {
	Key   string   `json:"key"`
	Text  *string  `json:"text"`
	Count *Counter `json:"count"`
	Flag  *bool    `json:"flag"`
}
type TraceSpan struct {
	TraceID      string           `json:"trace_id" pattern:"^[a-f0-9]{32}$"`
	ParentSpanID *string          `json:"parent_span_id" pattern:"^[a-f0-9]{16}$"`
	Kind         string           `json:"kind" enum:"agent,llm,tool,host,wait"`
	Name         string           `json:"name"`
	State        string           `json:"state" enum:"running,cancelling,reserved,dispatched,succeeded,failed,cancelled,interrupted,uncertain,waiting,ready,denied,pending,approved,answered,expired"`
	StartNS      Counter          `json:"start_ns"`
	EndNS        *Counter         `json:"end_ns"`
	Attributes   []TraceAttribute `json:"attributes"`
}
type TraceRow struct {
	Sequence   Counter    `json:"sequence"`
	RootID     ID         `json:"root_id"`
	SessionID  ID         `json:"session_id"`
	TurnID     ID         `json:"turn_id"`
	SourceKind string     `json:"source_kind" enum:"turn,attempt,cell,operation,permission,question"`
	SourceID   ID         `json:"source_id"`
	SpanID     string     `json:"span_id" pattern:"^[a-f0-9]{16}$"`
	Span       *TraceSpan `json:"span"`
}
type TracePageResult struct {
	ObservedAtNS Counter    `json:"observed_at_ns"`
	Items        []TraceRow `json:"items"`
	Revision     Counter    `json:"revision"`
	Next         Counter    `json:"next"`
	HasMore      bool       `json:"has_more"`
}
type TraceExportParams struct {
	RootID           ID       `json:"root_id"`
	TraceID          string   `json:"trace_id" pattern:"^([a-f0-9]{32})?$"`
	ExpectedRevision *Counter `json:"expected_revision"`
}
type TraceExportResult struct {
	Reference ContentReference `json:"reference"`
	Revision  Counter          `json:"revision"`
	Spans     int              `json:"spans" min:"0" max:"4096"`
	Traces    int              `json:"traces" min:"0" max:"4096"`
}

func traceSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[TracePageParams]():
		schema.Properties["after"].Type = "string"
		schema.Properties["after"].Types = nil
		for _, cursor := range []string{"after", "before"} {
			properties := make(map[string]*jsonschema.Schema, len(schema.Properties)-1)
			for name, field := range schema.Properties {
				if name != "after" && name != "before" || name == cursor {
					properties[name] = field.CloneSchemas()
				}
			}
			required := append(append([]string{}, schema.Required...), cursor)
			schema.OneOf = append(schema.OneOf, &jsonschema.Schema{Type: "object", Properties: properties, Required: required, AdditionalProperties: schema.AdditionalProperties.CloneSchemas()})
		}
		schema.Properties, schema.Required, schema.AdditionalProperties = nil, nil, nil
	case reflect.TypeFor[TracePageResult]():
		schema.Properties["items"].Type = "array"
		schema.Properties["items"].Types = nil
		schema.Properties["items"].MaxItems = new(2048)
	case reflect.TypeFor[TraceSpan]():
		schema.Properties["attributes"].Type = "array"
		schema.Properties["attributes"].Types = nil
		schema.Properties["attributes"].MaxItems = new(64)
		schema.Properties["name"].MaxLength = new(1024)
	case reflect.TypeFor[TraceAttribute]():
		schema.Properties["key"].MaxLength = new(128)
		schema.Properties["text"].MaxLength = new(2048)
		// An exact typed value: absent counters stay null, never fabricated zero.
		for _, key := range []string{"text", "count", "flag"} {
			branch := &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{}}
			for _, other := range []string{"text", "count", "flag"} {
				field := schema.Properties[other].CloneSchemas()
				if other == key {
					field.Types = nil
					switch key {
					case "text", "count":
						field.Type = "string"
					case "flag":
						field.Type = "boolean"
					}
				} else {
					field.Type = "null"
					field.Types = nil
				}
				branch.Properties[other] = field
			}
			schema.OneOf = append(schema.OneOf, branch)
		}
	}
}

func TracePageFromDomain(page session.TracePage) TracePageResult {
	result := TracePageResult{ObservedAtNS: Counter(page.ObservedAtNS), Items: []TraceRow{}, Revision: Counter(page.Revision), Next: Counter(page.Next), HasMore: page.HasMore}
	for _, row := range page.Items {
		item := TraceRow{Sequence: Counter(row.Sequence), RootID: ID(row.RootID), SessionID: ID(row.SessionID), TurnID: ID(row.TurnID), SourceKind: row.SourceKind, SourceID: ID(row.SourceID), SpanID: row.SpanID}
		if row.Span != nil {
			source := row.Span
			span := TraceSpan{TraceID: source.TraceID, ParentSpanID: source.ParentSpanID, Kind: source.Kind, Name: source.Name, State: source.State, StartNS: Counter(source.StartNS), Attributes: []TraceAttribute{}}
			if source.EndNS != nil {
				span.EndNS = new(Counter(*source.EndNS))
			}
			for _, attribute := range source.Attributes {
				value := TraceAttribute{Key: attribute.Key, Text: attribute.Text, Flag: attribute.Flag}
				if attribute.Count != nil {
					value.Count = new(Counter(*attribute.Count))
				}
				span.Attributes = append(span.Attributes, value)
			}
			item.Span = &span
		}
		result.Items = append(result.Items, item)
	}
	return result
}
