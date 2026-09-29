package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
)

func TestComputerBundledPublicationCASPreservesPolicyAndExistingPath(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	first, err := r.ComputerStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	settings := first.Config
	settings.Allow, settings.Deny, settings.DefaultDeny = []string{"editor"}, []string{"secrets"}, false
	before, err := r.ConfigureComputer(t.Context(), first.Revision, settings)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	publish := func(_ context.Context, directory string) (string, error) {
		calls++
		return filepath.Join(directory, "bin", "whip-computer"), nil
	}
	if _, err := r.useBundledComputer(t.Context(), first.Revision, publish); !errors.Is(err, config.ErrRevisionConflict) || calls != 0 {
		t.Fatal("stale revision extracted", err, calls)
	}
	after, err := r.useBundledComputer(t.Context(), before.Revision, publish)
	if err != nil || calls != 1 {
		t.Fatal(after, err, calls)
	}
	settings.HelperExecutable = filepath.Join(r.directory, "bin", "whip-computer")
	if !reflect.DeepEqual(after.Config, settings) || after.Config.Enabled || after.Control.State != "disabled" {
		t.Fatal("publication changed authority", after)
	}
	if _, err := r.useBundledComputer(t.Context(), after.Revision, publish); !errors.Is(err, config.ErrRevisionConflict) || calls != 1 {
		t.Fatal("existing path overwritten", err, calls)
	}
	if _, err := r.useBundledComputer(t.Context(), before.Revision, publish); !errors.Is(err, config.ErrRevisionConflict) || calls != 1 {
		t.Fatal("lost acknowledgment replayed", err, calls)
	}
	persisted, err := r.configuration.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(persisted.Host.Computer, settings) {
		t.Fatal(persisted, err)
	}
}

func TestComputerBundledPublicationCannotOverwriteConcurrentConfiguration(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	before, err := r.ComputerStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	publish := func(ctx context.Context, directory string) (string, error) {
		_, err := r.configuration.Update(ctx, before.Revision, func(host *config.Host) error {
			host.Computer.HelperExecutable = "/explicit/new-helper"
			host.Computer.Enabled = true
			return nil
		})
		return filepath.Join(directory, "bin", "whip-computer"), err
	}
	after, err := r.useBundledComputer(t.Context(), before.Revision, publish)
	if !errors.Is(err, config.ErrRevisionConflict) || !after.Config.Enabled || after.Config.HelperExecutable != "/explicit/new-helper" {
		t.Fatal(after, err)
	}
}

func TestComputerBundledPublicationFailureLeavesPolicyUntouched(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	before, err := r.ComputerStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("publication failed after filesystem rename")
	if _, err := r.useBundledComputer(t.Context(), before.Revision, func(context.Context, string) (string, error) { return "", failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	after, err := r.ComputerStatus(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
}
