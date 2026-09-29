package protocol

// MatchReceiptParams checks the original typed request using the same Go-owned
// digest construction as admission. It never submits a missing request.
type MatchReceiptParams struct {
	Method       string `json:"method" enum:"sessions.submit,sessions.compact,sessions.spawn,goals.formulate,goals.resume,tool.call,shell.run"`
	ParamsBase64 string `json:"params_base64"`
}
