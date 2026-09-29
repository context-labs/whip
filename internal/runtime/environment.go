package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

// Host identity is local account metadata, never caller-supplied environment
// variables. It is captured once, contains no home path or credentials, and does
// not authorize any operation.
var instructionUsername = sync.OnceValue(func() string {
	current, err := user.Current()
	if err != nil || current.Username == "" || len(current.Username) > 256 || !utf8.ValidString(current.Username) {
		return "unavailable"
	}
	return current.Username
})

func environmentInstructions(owner session.Session, turn session.Turn) string {
	// JSON quotes also escape markup and control characters in path/account data.
	quoted := func(value string) string { raw, _ := json.Marshal(value); return string(raw) }
	identity := fmt.Sprintf("Identity: root agent (session %s, tree %s, definition %s at revision %s).", owner.ID, owner.TreeID, owner.Definition.ID, owner.Definition.Revision)
	if owner.ParentID != nil {
		identity = fmt.Sprintf("Identity: child agent %s (session %s, tree %s, parent session %s, definition %s at revision %s).", quoted(owner.Name), owner.ID, owner.TreeID, *owner.ParentID, owner.Definition.ID, owner.Definition.Revision)
		switch owner.Config.ReportMode {
		case session.ReportNotice:
			identity += " Successful completion sends your parent a short automatic notice with evidence for the full result."
		case session.ReportInline:
			identity += " Successful completion sends your parent an automatic notice with a bounded preview of your final answer and evidence for the full result."
		case session.ReportMessage:
			identity += " Successful completion sends no automatic notice; reporting to your parent requires explicit mail.send authority and delivery."
		}
	}
	return fmt.Sprintf("%s\n\nEnvironment:\n<env>\n  Platform: %s/%s\n  Current date/time: %s (captured at turn start)\n  User: %s (uid %d)\n  Working directory: %s\n</env>\nThese environment facts do not grant filesystem, process, network or delegation authority.", identity, runtime.GOOS, runtime.GOARCH, turn.StartedAt.UTC().Format(time.RFC3339Nano), quoted(instructionUsername()), os.Getuid(), quoted(owner.WorkingDirectory))
}
