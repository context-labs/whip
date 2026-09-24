package protocol

// Terminal operations are connection-scoped and ephemeral: a workspace terminal
// tab owns one shell on the execution host. Output travels as notifications to
// the attached connection; a lost reply is answered by attaching again, never by
// replaying a durable command.

// TerminalOpenParams starts a login shell. Cwd is optional and must be an
// absolute directory on the execution host; RootID is a hint for the daemon
// to default Cwd to that session's working directory.
type TerminalOpenParams struct {
	Cwd    string `json:"cwd,omitempty"`
	RootID string `json:"root_id,omitempty"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

type TerminalOpenResult struct {
	ID    string `json:"id"`
	Shell string `json:"shell"`
	Cwd   string `json:"cwd"`
}

// TerminalAttachParams makes this connection the terminal's live receiver and
// replays retained output from Cursor. A negative Cursor asks for live output
// only; a cursor older than the retained ring is clamped to the ring start.
type TerminalAttachParams struct {
	ID     string `json:"id"`
	Cursor int64  `json:"cursor,string"`
}

// TerminalAttachResult reports where replay starts and the terminal's state.
// Exited terminals still replay; their exit notification follows the replay.
type TerminalAttachResult struct {
	Cursor   int64  `json:"cursor,string"`
	Cwd      string `json:"cwd"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Exited   bool   `json:"exited"`
	ExitCode int    `json:"exit_code,omitempty"`
	Signal   string `json:"signal,omitempty"`
}

// TerminalWriteParams sends raw keystrokes; Bytes is base64 on the wire.
type TerminalWriteParams struct {
	ID    string `json:"id"`
	Bytes []byte `json:"bytes"`
}

type TerminalResizeParams struct {
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type TerminalIDParams struct {
	ID string `json:"id"`
}

// TerminalOutputParams is the terminal.output notification. Cursor is the
// absolute offset of the first byte, so a client can resume from cursor+len.
type TerminalOutputParams struct {
	ID     string `json:"id"`
	Cursor int64  `json:"cursor,string"`
	Bytes  []byte `json:"bytes"`
}

// TerminalExitedParams is the terminal.exited notification, sent after the last
// output of an exited terminal. ExitCode is -1 when Signal names the killer.
type TerminalExitedParams struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exit_code"`
	Signal   string `json:"signal,omitempty"`
}

// TerminalDetachedParams is the terminal.detached notification: another
// connection attached, so this one no longer receives output.
type TerminalDetachedParams struct {
	ID string `json:"id"`
}
