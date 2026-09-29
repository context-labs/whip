package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/theme"
)

// NativeOptions contains explicit client launch intent. Host declarations,
// effective session configuration and instructions remain owned by the host.
type NativeOptions struct {
	Model, Provider, Resume, Engine, Agent string
	WorkingDirectory, ClientHome           string
	InitialPrompt                          string
	Cautious, Automatic                    bool
}

// RunNative borrows a pinned native connection. Detaching joins terminal work;
// accepted inputs and the host continue independently of this terminal.
func RunNative(ctx context.Context, connection *client.Client, options NativeOptions, programOptions ...tea.ProgramOption) (string, error) {
	if connection == nil {
		return "", errors.New("native client is required")
	}
	if err := options.validate(); err != nil {
		return "", err
	}
	directory, err := nativeClientDirectory(options.ClientHome)
	if err != nil {
		return "", err
	}
	preferences, err := readNativePreferences(directory)
	if err != nil {
		return "", err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	owner, err := configureNativeSession(bounded, connection, options)
	cancel()
	if err != nil {
		return string(owner.ID), err
	}
	m, err := newNativeModel(ctx, connection, owner)
	if err != nil {
		return string(owner.ID), err
	}
	defer m.close()
	m.notesHome, m.preferencesDirectory, m.preferences = options.ClientHome, directory, preferences
	m.showReasoning = nativePreferenceLabel(preferences.Thinking, true) == "on"
	m.initialPrompt = options.InitialPrompt
	m.input.SetValue(options.InitialPrompt)
	m.status = "Session " + string(owner.ID) + " · /setup /model /sessions /settings · Ctrl+C twice detaches"
	specs, failures := theme.Load(directory)
	themeMu.Lock()
	userThemes = specs
	themeMu.Unlock()
	setSchemeOverride(preferences.Theme)
	if err := errors.Join(failures...); err != nil {
		m.status += ". Some local themes could not be read: " + err.Error()
	}
	if owner.Configuration.Model.Provider == "" || owner.Configuration.Model.Name == "" {
		m.menu = newNativeMenu(&m.work, connection, nativeMenuOptions{Kind: "setup", Owner: &owner, PreferencesDirectory: directory})
	}
	programOptions = append(programOptions, tea.WithContext(ctx))
	_, err = tea.NewProgram(m, programOptions...).Run()
	return string(m.owner.ID), err
}

func (o NativeOptions) validate() error {
	if o.Cautious && o.Automatic {
		return errors.New("cautious and automatic modes are mutually exclusive")
	}
	if o.Engine != "" && o.Engine != "starlark" && o.Engine != "quickjs" {
		return errors.New("unknown execution engine")
	}
	if !filepath.IsAbs(o.ClientHome) {
		return errors.New("an explicit absolute client home is required")
	}
	if o.Resume == "" && !filepath.IsAbs(o.WorkingDirectory) {
		return errors.New("an absolute working directory is required for a new session")
	}
	return nil
}

// Client preferences never use a remote session's directory or retired files.
func nativeClientDirectory(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", errors.New("an explicit absolute client home is required")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	const name = "client-v4"
	if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() {
		return "", errors.New("native client directory must be a directory, not a symbolic link")
	}
	return filepath.Join(home, name), nil
}

func configureNativeSession(ctx context.Context, connection *client.Client, options NativeOptions) (protocol.Session, error) {
	var owner protocol.Session
	mode := ""
	if options.Cautious {
		mode = "prompt"
	} else if options.Automatic {
		mode = "automatic"
	}
	if options.Resume == "" {
		ref, err := connection.ResolveDefinition(ctx, options.Agent)
		if err != nil {
			return owner, err
		}
		var model *protocol.ModelSelection
		if options.Model != "" || options.Provider != "" {
			var inventory protocol.ProviderInventory
			if err := connection.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
				return owner, err
			}
			selection := protocol.ModelSelection{}
			if inventory.Defaults != nil {
				selection = *inventory.Defaults
			}
			model = new(nativeLaunchModel(selection, options))
		}
		params := protocol.CreateTreeParams{CreationID: protocol.ID(uuid.NewString()), Definition: ref, Engine: options.Engine, WorkingDirectory: options.WorkingDirectory, Overrides: protocol.ConfigPatch{Model: model}}
		if mode != "" {
			params.PermissionMode = new(mode)
		}
		var created protocol.CreateTreeResult
		if err := connection.Call(ctx, "trees.create", params, &created); err != nil {
			// Receipt lookup only: an unknown admission is never sent twice here.
			if _, rejected := errors.AsType[*client.Error](err); rejected {
				return owner, err
			}
			if checkErr := connection.Call(ctx, "trees.creation", protocol.TreeCreationParams{CreationID: params.CreationID}, &created); checkErr != nil {
				return owner, fmt.Errorf("creation %s may have been accepted; inspect that original creation before starting another session: %w", params.CreationID, errors.Join(err, checkErr))
			}
		}
		if created.Creation.ID != params.CreationID || created.Deleted || created.Root == nil || created.Tree == nil || created.Root.ID != created.Creation.RootID || created.Root.TreeID != created.Creation.TreeID || created.Tree.ID != created.Root.TreeID {
			return owner, errors.New("created session was deleted or has mismatched identity")
		}
		return *created.Root, nil
	}
	var err error
	owner, err = resolveNativeSession(ctx, connection, options.Resume)
	if err != nil {
		return owner, err
	}
	if options.Agent != "" && options.Agent != string(owner.Definition.ID) && options.Agent != string(owner.Definition.ID)+"@"+owner.Definition.Revision {
		return owner, errors.New("resumed session has a different immutable agent definition")
	}
	if options.Engine != "" {
		var tree protocol.Tree
		if err := connection.Call(ctx, "trees.get", protocol.TreeParams{TreeID: owner.TreeID}, &tree); err != nil {
			return owner, err
		}
		if tree.ID != owner.TreeID || tree.Engine != options.Engine {
			return owner, errors.New("resumed session has a different immutable engine")
		}
	}
	if options.Model != "" || options.Provider != "" {
		params := protocol.UpdateConfigurationParams{SessionID: owner.ID, ExpectedRevision: owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: new(nativeLaunchModel(owner.Configuration.Model, options))}}
		var updated protocol.Session
		if err := connection.Call(ctx, "sessions.configure", params, &updated); err != nil {
			return owner, fmt.Errorf("configure session %s; inspect current configuration before repeating an unknown edit: %w", owner.ID, err)
		}
		if updated.ID != owner.ID || updated.ConfigRevision <= owner.ConfigRevision {
			return owner, errors.New("updated session identity or configuration revision mismatch")
		}
		owner = updated
	}
	if mode != "" {
		var policy protocol.PermissionPolicy
		if err := connection.Call(ctx, "permissions.policy", protocol.SessionParams{SessionID: owner.ID}, &policy); err != nil {
			return owner, err
		}
		if policy.TreeID != owner.TreeID {
			return owner, errors.New("permission policy ownership mismatch")
		}
		params := protocol.SetPermissionModeParams{EditID: protocol.ID(uuid.NewString()), SessionID: owner.ID, ExpectedRevision: policy.Revision, Mode: mode}
		var edit protocol.PermissionModeEdit
		if err := connection.Call(ctx, "permissions.set_mode", params, &edit); err != nil {
			return owner, fmt.Errorf("permission edit %s for session %s may require inspection: %w", params.EditID, owner.ID, err)
		}
		if edit.ID != params.EditID || edit.SessionID != owner.ID || edit.Mode != mode || edit.Policy.TreeID != owner.TreeID {
			return owner, errors.New("permission edit identity mismatch")
		}
	}
	return owner, nil
}

func nativeLaunchModel(current protocol.ModelSelection, options NativeOptions) protocol.ModelSelection {
	if options.Model != "" {
		current.Name = options.Model
	}
	if options.Provider != "" {
		current.Provider = protocol.ID(options.Provider)
	}
	return current
}
