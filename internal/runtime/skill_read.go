package runtime

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

type skillReadRequest struct {
	Scope  string  `json:"scope"`
	RootID *string `json:"root_id"`
	Name   string  `json:"name"`
	Offset int64   `json:"offset,string"`
	Length int     `json:"length"`
	SHA256 *string `json:"sha256"`
}

type skillReadPage struct {
	Name       string                    `json:"name"`
	Source     session.InstructionSource `json:"source"`
	SHA256     string                    `json:"sha256"`
	TotalBytes int64                     `json:"total_bytes,string"`
	Offset     int64                     `json:"offset,string"`
	DataBase64 string                    `json:"data_base64"`
	NextOffset *int64                    `json:"next_offset,string"`
}

func (r *Runtime) prepareSkillRead(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if call.Module != "skills" || call.Name != "read" || call.SessionID != current.ID {
		return tool.Prepared{}, session.ErrInvalid
	}
	args := skillReadRequest{Length: 65536}
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, err
	}
	if err := args.validate(); err != nil {
		return tool.Prepared{}, err
	}
	cell, err := r.store.Cell(ctx, call.CellID)
	if err != nil {
		return tool.Prepared{}, err
	}
	if cell.SessionID != current.ID {
		return tool.Prepared{}, fmt.Errorf("%w: skill read cell owner mismatch", session.ErrInvalid)
	}
	turn, err := r.store.Turn(ctx, cell.TurnID)
	if err != nil {
		return tool.Prepared{}, err
	}
	if turn.SessionID != current.ID {
		return tool.Prepared{}, session.ErrInvalid
	}
	configuration, err := r.store.Configuration(ctx, current.ID, turn.ConfigRevision)
	if err != nil {
		return tool.Prepared{}, err
	}
	path, rootID := current.WorkingDirectory, ""
	capability, resource := "files.read", path
	switch args.Scope {
	case "project":
		rootID = *args.RootID
		if configuration.Instructions.ProjectRoot == nil || *configuration.Instructions.ProjectRoot != rootID {
			return tool.Prepared{}, fmt.Errorf("%w: project root is not selected by this turn", session.ErrInvalid)
		}
		var registered bool
		path, registered = r.host.ProjectRoots[rootID]
		if !registered {
			return tool.Prepared{}, fmt.Errorf("%w: unknown project instruction root", session.ErrInvalid)
		}
		capability, resource = "instructions.read", "project:"+rootID
	case "host":
		rootID = *args.RootID
		if !slices.Contains(configuration.Instructions.SkillRoots, rootID) {
			return tool.Prepared{}, fmt.Errorf("%w: skill root is not selected by this turn", session.ErrInvalid)
		}
		var registered bool
		path, registered = r.host.SkillRoots[rootID]
		if !registered {
			return tool.Prepared{}, fmt.Errorf("%w: unknown skill root", session.ErrInvalid)
		}
		capability, resource = "skills.read", rootID
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		return tool.Prepared{}, err
	}
	execution := &skillReadExecution{path: path, cwd: current.WorkingDirectory, rootID: rootID, request: args}
	return tool.Prepared{Capability: capability, Resource: resource, Arguments: arguments, Acquire: execution.acquire, Run: execution.run}, nil
}

func (r skillReadRequest) validate() error {
	if (r.Scope != "workspace" && r.Scope != "host" && r.Scope != "project") || (r.Scope == "workspace") != (r.RootID == nil) {
		return fmt.Errorf("%w: skill scope and root_id must identify workspace, host or project", session.ErrInvalid)
	}
	if err := session.ValidateText(r.Name, 64); err != nil {
		return err
	}
	if !utf8.ValidString(r.Name) || strings.ContainsAny(r.Name, `/\`) || r.Offset < 0 || r.Offset > session.MaxInvokedSkillBytes || r.Length < 1 || r.Length > 65536 {
		return fmt.Errorf("%w: invalid skill name or byte range", session.ErrInvalid)
	}
	if r.RootID != nil {
		if err := session.ValidateID(*r.RootID); err != nil {
			return err
		}
	}
	if r.SHA256 == nil {
		if r.Offset != 0 {
			return fmt.Errorf("%w: skill continuation requires sha256", session.ErrInvalid)
		}
		return nil
	}
	digest, err := hex.DecodeString(*r.SHA256)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != *r.SHA256 {
		return fmt.Errorf("%w: skill sha256 must be lowercase SHA-256", session.ErrInvalid)
	}
	return nil
}

// A prepared read owns one acquired root. Dispatch remains the SQL authority
// boundary; acquiring a descriptor never reads the catalog or a skill body.
type skillReadExecution struct {
	path    string
	cwd     string
	project *instruction.Root
	rootID  string
	request skillReadRequest
	root    *os.Root
}

func (e *skillReadExecution) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.request.Scope == "project" {
		root, err := openProjectInstructions(ctx, e.rootID, e.path, e.cwd)
		if err != nil {
			return nil, err
		}
		if root == nil {
			return nil, errors.New("project instruction boundary does not contain this workspace")
		}
		e.project, e.root = root, root.FS
		return func() { _ = root.FS.Close(); e.project, e.root = nil, nil }, nil
	}
	root, err := os.OpenRoot(e.path)
	if err != nil {
		// Host paths are operator-only configuration, including in failures.
		return nil, errors.New("skill root is unavailable")
	}
	e.root = root
	return func() {
		_ = root.Close()
		e.root = nil
	}, nil
}

func (e *skillReadExecution) run(ctx context.Context, _ session.OperationID) (any, error) {
	if e.root == nil {
		return nil, errors.New("skill read requires an acquired root")
	}
	root := instruction.Root{ID: e.rootID, FS: e.root}
	if e.project != nil {
		root = *e.project
	}
	catalog, err := instruction.Catalog(ctx, []instruction.Root{root})
	if err != nil {
		return nil, err
	}
	for _, selected := range catalog.Skills {
		if selected.Name != e.request.Name {
			continue
		}
		text, source, err := instruction.ReadSkill(ctx, e.root, selected)
		if err != nil {
			return nil, err
		}
		if e.request.SHA256 != nil && source.SHA256 != *e.request.SHA256 {
			return nil, errors.New("skill changed since the requested digest")
		}
		if e.request.Offset > int64(len(text)) {
			return nil, fmt.Errorf("%w: skill offset exceeds total bytes", session.ErrInvalid)
		}
		end := min(int64(len(text)), e.request.Offset+int64(e.request.Length))
		page := skillReadPage{Name: selected.Name, Source: source, SHA256: source.SHA256, TotalBytes: int64(len(text)), Offset: e.request.Offset, DataBase64: base64.StdEncoding.EncodeToString([]byte(text[e.request.Offset:end]))}
		if end < int64(len(text)) {
			page.NextOffset = &end
		}
		return page, nil
	}
	return nil, errors.New("skill name was not found in the selected root")
}
