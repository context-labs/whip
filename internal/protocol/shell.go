package protocol

type ShellInteractionParams struct {
	SessionID ID      `json:"session_id"`
	Cursor    Counter `json:"cursor"`
}

type ShellInteraction struct {
	OperationID ID      `json:"operation_id"`
	StartedAt   string  `json:"started_at"`
	DataBase64  string  `json:"data_base64"`
	From        Counter `json:"from"`
	Through     Counter `json:"through"`
	NextInput   Counter `json:"next_input"`
	SecondsLeft int     `json:"seconds_left" min:"0" max:"15"`
}

type ShellInteractionResult struct {
	Interaction *ShellInteraction `json:"interaction"`
}

type ShellInputParams struct {
	SessionID   ID      `json:"session_id"`
	OperationID ID      `json:"operation_id"`
	Sequence    Counter `json:"sequence"`
	DataBase64  string  `json:"data_base64"`
}

type ShellInputResult struct {
	Sequence Counter `json:"sequence"`
}
