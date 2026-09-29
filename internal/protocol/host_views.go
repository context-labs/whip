package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type WorkspaceCompletionParams struct {
	SessionID ID     `json:"session_id"`
	Kind      string `json:"kind" enum:"mention,path"`
	Prefix    string `json:"prefix"`
	Limit     int    `json:"limit" min:"1" max:"64"`
}
type WorkspaceCompletionCandidate struct {
	Text        string `json:"text"`
	Description string `json:"description" enum:",dir"`
}
type WorkspaceCompletionResult struct {
	WorkingDirectory string                         `json:"working_directory"`
	Candidates       []WorkspaceCompletionCandidate `json:"candidates"`
	Truncated        bool                           `json:"truncated"`
}

type HostDirectoriesParams struct {
	Path       string `json:"path"`
	After      string `json:"after"`
	Prefix     string `json:"prefix"`
	ShowHidden bool   `json:"show_hidden"`
	Limit      int    `json:"limit" min:"1" max:"128"`
}
type HostDirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type HostDirectoriesResult struct {
	Path      string               `json:"path"`
	Parent    string               `json:"parent"`
	Entries   []HostDirectoryEntry `json:"entries"`
	NextAfter *string              `json:"next_after"`
	HasMore   bool                 `json:"has_more"`
	Truncated bool                 `json:"truncated"`
}
type HostDirectoryPickParams struct {
	Start string `json:"start"`
}
type HostDirectoryCreateParams struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}
type HostDirectoryCreateResult struct {
	Path string `json:"path"`
}
type HostDirectoryPickResult struct {
	Path      *string `json:"path"`
	Cancelled bool    `json:"cancelled"`
}
type HostSkillsParams struct {
	Scope      string         `json:"scope" enum:"global,project"`
	CWD        string         `json:"cwd"`
	Prefix     string         `json:"prefix"`
	Definition *DefinitionRef `json:"definition"`
	Limit      int            `json:"limit" min:"1" max:"1024"`
}
type HostSkillCandidate struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}
type HostSkillsResult struct {
	Candidates []HostSkillCandidate `json:"candidates"`
	Truncated  bool                 `json:"truncated"`
}
type HostThemeResolveParams struct {
	Name string `json:"name"`
	JSON string `json:"json"`
}
type HostThemeMetadata struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Dark   bool   `json:"dark"`
	Source string `json:"source" enum:"builtin,custom"`
}
type HostThemeError struct {
	File    string `json:"file"`
	Message string `json:"message"`
}
type HostThemesResult struct {
	Themes    []HostThemeMetadata `json:"themes"`
	Errors    []HostThemeError    `json:"errors"`
	Truncated bool                `json:"truncated"`
}

type HostThemeColors struct {
	Background  string `json:"background"`
	Foreground  string `json:"foreground"`
	Muted       string `json:"muted"`
	Faint       string `json:"faint"`
	Primary     string `json:"primary"`
	OnPrimary   string `json:"on_primary"`
	Accent      string `json:"accent"`
	Success     string `json:"success"`
	Warning     string `json:"warning"`
	Error       string `json:"error"`
	Info        string `json:"info"`
	Link        string `json:"link"`
	Emphasis    string `json:"emphasis"`
	Border      string `json:"border"`
	BorderFocus string `json:"border_focus"`
	DiffAdd     string `json:"diff_add"`
	DiffDel     string `json:"diff_del"`
	Panel       string `json:"panel"`
	Element     string `json:"element"`
	Hover       string `json:"hover"`
}

type HostThemeTokenStyle struct {
	Color      string `json:"color"`
	Background string `json:"background"`
	Bold       bool   `json:"bold"`
	Italic     bool   `json:"italic"`
	Underline  bool   `json:"underline"`
}

type HostThemeCodeStyle struct {
	Foreground string                         `json:"foreground"`
	Background string                         `json:"background"`
	Tokens     map[string]HostThemeTokenStyle `json:"tokens"`
}

type HostThemeWebColors struct {
	Navigation           string `json:"navigation,omitempty"`
	QuietBorder          string `json:"quiet_border,omitempty"`
	CodeBackground       string `json:"code_background,omitempty"`
	InlineCodeBackground string `json:"inline_code_background,omitempty"`
}

type HostThemeResolved struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Dark     bool                  `json:"dark"`
	Colors   HostThemeColors       `json:"colors"`
	Syntax   HostThemeSyntaxSpec   `json:"syntax"`
	Markdown HostThemeMarkdownSpec `json:"markdown"`
	Code     HostThemeCodeStyle    `json:"code"`
	Web      *HostThemeWebColors   `json:"web,omitempty"`
}

type HostThemeSyntaxSpec struct {
	Keyword     string `json:"keyword"`
	String      string `json:"string"`
	Number      string `json:"number"`
	Comment     string `json:"comment"`
	Function    string `json:"function"`
	Type        string `json:"type"`
	Operator    string `json:"operator"`
	Punctuation string `json:"punctuation"`
}

type HostThemeMarkdownSpec struct {
	Heading string `json:"heading"` // else Accent
	Strong  string `json:"strong"`  // else Warning
	Code    string `json:"code"`    // else Success
	Quote   string `json:"quote"`   // else Muted
}

func hostViewsSchema(schema *jsonschema.Schema, t reflect.Type) {
	bound := func(field string, n int) { schema.Properties[field].MaxLength = new(n) }
	array := func(field string, n int) {
		value := schema.Properties[field]
		value.Type = "array"
		value.Types = nil
		value.MaxItems = new(n)
	}
	switch t {
	case reflect.TypeFor[WorkspaceCompletionParams]():
		bound("prefix", 4096)
	case reflect.TypeFor[WorkspaceCompletionCandidate]():
		bound("text", 8192)
	case reflect.TypeFor[WorkspaceCompletionResult]():
		bound("working_directory", 4096)
		array("candidates", 64)
	case reflect.TypeFor[HostDirectoriesParams]():
		bound("path", 4096)
		bound("after", 256)
		bound("prefix", 256)
	case reflect.TypeFor[HostDirectoryPickParams]():
		bound("start", 4096)
	case reflect.TypeFor[HostDirectoryCreateParams]():
		bound("parent", 4096)
		bound("name", 255)
		schema.Properties["parent"].MinLength = new(1)
		schema.Properties["name"].MinLength = new(1)
	case reflect.TypeFor[HostDirectoryCreateResult]():
		bound("path", 4096)
		schema.Properties["path"].MinLength = new(1)
	case reflect.TypeFor[HostDirectoriesResult]():
		array("entries", 128)
	case reflect.TypeFor[HostSkillsParams]():
		bound("cwd", 4096)
		bound("prefix", 4096)
	case reflect.TypeFor[HostSkillsResult]():
		array("candidates", 1024)
	case reflect.TypeFor[HostThemeResolveParams]():
		bound("name", 256)
		bound("json", 65536)
	case reflect.TypeFor[HostThemesResult]():
		array("themes", 256)
		array("errors", 128)
	case reflect.TypeFor[HostThemeCodeStyle]():
		schema.Properties["tokens"].MaxProperties = new(512)
	}
}
