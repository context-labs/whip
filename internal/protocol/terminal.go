package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Human terminal identities exist only in this process generation. They are not
// sessions, model operations, durable receipts or restorable PTY identities.
type TerminalRef struct {
	ProcessEpoch ID `json:"process_epoch"`
	ID           ID `json:"id"`
}
type TerminalOpenParams struct {
	ProcessEpoch ID     `json:"process_epoch"`
	Cwd          string `json:"cwd"`
	Cols         uint16 `json:"cols" min:"1" max:"1000"`
	Rows         uint16 `json:"rows" min:"1" max:"1000"`
}
type TerminalReadParams struct {
	ProcessEpoch ID      `json:"process_epoch"`
	ID           ID      `json:"id"`
	Cursor       Counter `json:"cursor"`
	Limit        int     `json:"limit" min:"1" max:"32768"`
}
type TerminalWriteParams struct {
	ProcessEpoch ID     `json:"process_epoch"`
	ID           ID     `json:"id"`
	DataBase64   string `json:"data_base64"`
}
type TerminalResizeParams struct {
	ProcessEpoch ID     `json:"process_epoch"`
	ID           ID     `json:"id"`
	Cols         uint16 `json:"cols" min:"1" max:"1000"`
	Rows         uint16 `json:"rows" min:"1" max:"1000"`
}
type TerminalListParams struct {
	ProcessEpoch ID `json:"process_epoch"`
}
type TerminalInfo struct {
	ProcessEpoch ID      `json:"process_epoch"`
	ID           ID      `json:"id"`
	Cwd          string  `json:"cwd"`
	Shell        string  `json:"shell"`
	Cols         int     `json:"cols" min:"1" max:"1000"`
	Rows         int     `json:"rows" min:"1" max:"1000"`
	Closing      bool    `json:"closing"`
	Exited       bool    `json:"exited"`
	ExitCode     int     `json:"exit_code"`
	Signal       string  `json:"signal"`
	Start        Counter `json:"start"`
	End          Counter `json:"end"`
	CreatedAt    string  `json:"created_at"`
}
type TerminalPage struct {
	Terminal   TerminalInfo `json:"terminal"`
	From       Counter      `json:"from"`
	Next       Counter      `json:"next"`
	End        Counter      `json:"end"`
	Truncated  bool         `json:"truncated"`
	DataBase64 string       `json:"data_base64"`
}
type TerminalList struct {
	ProcessEpoch ID             `json:"process_epoch"`
	Items        []TerminalInfo `json:"items"`
}
type TerminalAccepted struct {
	Accepted bool `json:"accepted"`
}

func terminalSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[TerminalOpenParams]():
		schema.Properties["cwd"].MinLength = new(1)
		schema.Properties["cwd"].MaxLength = new(4096)
	case reflect.TypeFor[TerminalWriteParams]():
		schema.Properties["data_base64"].MinLength = new(4)
		schema.Properties["data_base64"].MaxLength = new(21848)
	case reflect.TypeFor[TerminalPage]():
		schema.Properties["data_base64"].MaxLength = new(43692)
	case reflect.TypeFor[TerminalList]():
		schema.Properties["items"].Types = nil
		schema.Properties["items"].Type = "array"
		schema.Properties["items"].MaxItems = new(16)
	}
}
