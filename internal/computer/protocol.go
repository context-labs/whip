package computer

import (
	"encoding/base64"
	"encoding/json"
)

// ProtocolVersion must match Protocol.version in the Swift driver.
const ProtocolVersion = "whip-computer/1"

// tokenEnvVar must match Protocol.tokenEnvVar in the Swift driver.
const tokenEnvVar = "WHIP_COMPUTER_TOKEN"

// Application-level JSON-RPC error codes (mirror RPCErrorCode in the driver).
const (
	errCodeUnknownApp      = 1
	errCodeNoAXPermission  = 2
	errCodeNoScreenPerm    = 3
	errCodeStaleGeneration = 4
	errCodeIndexOutOfRange = 5
	errCodeNotActionable   = 6
	errCodeScreenLocked    = 7
	errCodeBadToken        = 8
)

// rpcError is a JSON-RPC error object from the helper.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcResponse is a newline-delimited helper response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// AXElement is one indexed node in a state()/ax() response.
type AXElement struct {
	Index    int       `json:"index"`
	Role     string    `json:"role"`
	Subrole  string    `json:"subrole,omitempty"`
	Title    string    `json:"title,omitempty"`
	Value    string    `json:"value,omitempty"`
	Desc     string    `json:"desc,omitempty"`
	RoleDesc string    `json:"roleDescription,omitempty"`
	Position []float64 `json:"position,omitempty"` // [x, y] points
	Size     []float64 `json:"size,omitempty"`     // [w, h] points
	Actions  []string  `json:"actions,omitempty"`
	Focused  bool      `json:"focused"`
	Enabled  bool      `json:"enabled"`
}

// Screenshot is an inline JPEG frame normalized to point resolution.
type Screenshot struct {
	JPEGBase64 string `json:"jpegBase64,omitempty"`
	Bytes      int    `json:"bytes,omitempty"`
	Err        string `json:"error,omitempty"`
}

// AppState is the result of state(): fresh AX tree + screenshot in-call.
type AppState struct {
	Generation int         `json:"generation"`
	App        string      `json:"app"`
	Elements   []AXElement `json:"elements"`
	Screenshot *Screenshot `json:"screenshot,omitempty"`
}

// RunningApp is one entry of apps().
type RunningApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundleId"`
	PID      int    `json:"pid"`
	Active   bool   `json:"active"`
}

// TCCStatus reports the helper's permission grants.
type TCCStatus struct {
	Accessibility   bool   `json:"accessibility"`
	ScreenRecording bool   `json:"screenRecording"`
	Pending         bool   `json:"pending,omitempty"`
	Hint            string `json:"hint,omitempty"`
}

// Decode returns JPEG bytes, or nil when the screenshot is absent.
func (s *Screenshot) Decode() ([]byte, error) {
	if s == nil || s.JPEGBase64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s.JPEGBase64)
}
