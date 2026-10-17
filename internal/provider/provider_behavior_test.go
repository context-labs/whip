package provider

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func TestProviderConfigurationRejectsInvalidChangesAtomically(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "validation")
	defer service.Close()
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change protocol.ConfigurationUpdate
	}{
		{"missing revision", protocol.ConfigurationUpdate{}},
		{"effort", protocol.ConfigurationUpdate{Revision: before.Revision, DefaultEffort: new("turbo")}},
		{"negative compact", protocol.ConfigurationUpdate{Revision: before.Revision, CompactPercent: new(-1)}},
		{"large compact", protocol.ConfigurationUpdate{Revision: before.Revision, CompactPercent: new(101)}},
		{"rounds", protocol.ConfigurationUpdate{Revision: before.Revision, GoalMaxRounds: new(-1)}},
		{"retries", protocol.ConfigurationUpdate{Revision: before.Revision, MaxRetries: new(-1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UpdateConfiguration(tc.change); err == nil {
				t.Fatal("invalid update accepted")
			}
			after, err := service.ReadConfiguration()
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("failed update changed configuration: %+v %v", after, err)
			}
		})
	}
	for _, p := range []protocol.ProviderKeySetup{{}, {Revision: before.Revision, Provider: "unknown"}, {Revision: before.Revision, Provider: "openrouter"}} {
		if _, err := service.SetProviderKey(t.Context(), p); err == nil {
			t.Fatal("invalid key setup accepted")
		}
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, errors.New("provider leaked secret-key")
	}
	if _, err := service.SetProviderKey(t.Context(), protocol.ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "key"}); err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("unsafe validation failure: %v", err)
	}
	if _, err := service.LoginStatus(""); err == nil {
		t.Fatal("empty login identity accepted")
	}
	if _, err := service.CancelLogin("missing"); err == nil {
		t.Fatal("missing login cancelled")
	}
	if _, err := service.SelectLoginTeam("missing", "team"); err == nil {
		t.Fatal("missing login selected team")
	}
	if _, err := service.SelectLoginProject("missing", "project"); err == nil {
		t.Fatal("missing login selected project")
	}
	if _, err := service.CreateLoginProject("missing", ""); err == nil {
		t.Fatal("blank project name accepted")
	}
	service.Close()
	if _, err := service.BeginLogin(); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed login service: %v", err)
	}
}

func TestProviderLoginFailuresAndCancellationNeverReportSuccess(t *testing.T) {
	for _, stage := range []string{"projects", "create", "finish", "cancel_provisioning", "cancel_projects"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("WHIPCODE_HOME", t.TempDir())
			service := NewProviderService(t.Context(), "failure")
			defer service.Close()
			entered := make(chan struct{})
			service.login = func(context.Context, func(string, string)) (providerLoginIdentity, error) {
				return providerLoginIdentity{token: "private-token", teams: []inferencenet.Team{{ID: "team"}, {ID: "other"}}}, nil
			}
			service.projects = func(ctx context.Context, _ string, _ inferencenet.Team) ([]inferencenet.Project, error) {
				if stage == "projects" {
					return nil, errors.New("upstream leaked private-token")
				}
				if stage == "cancel_projects" {
					close(entered)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []inferencenet.Project{{ID: "project"}, {ID: "other"}}, nil
			}
			service.create = func(context.Context, string, inferencenet.Team, string) (inferencenet.Project, error) {
				return inferencenet.Project{}, errors.New("upstream leaked private-token")
			}
			service.finish = func(ctx context.Context, _ inferencenet.Auth) error {
				if stage == "cancel_provisioning" {
					close(entered)
					<-ctx.Done()
					return ctx.Err()
				}
				return errors.New("upstream leaked private-token")
			}
			flow, err := service.BeginLogin()
			if err != nil {
				t.Fatal(err)
			}
			waitProviderState(t, service, flow.FlowID, "choose_team")
			if _, err := service.SelectLoginTeam(flow.FlowID, "team"); err != nil {
				t.Fatal(err)
			}
			if stage != "projects" && stage != "cancel_projects" {
				waitProviderState(t, service, flow.FlowID, "choose_project")
				if stage == "create" {
					_, err = service.CreateLoginProject(flow.FlowID, "created")
				} else {
					_, err = service.SelectLoginProject(flow.FlowID, "project")
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			want := "failed"
			if strings.HasPrefix(stage, "cancel_") {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("provider operation did not begin")
				}
				if _, err := service.CancelLogin(flow.FlowID); err != nil {
					t.Fatal(err)
				}
				want = "cancelled"
				if stage == "cancel_provisioning" {
					want = "interrupted"
				}
			}
			status := waitProviderState(t, service, flow.FlowID, want)
			service.Close()
			final, err := service.LoginStatus(flow.FlowID)
			if err != nil || final.State != want || final.ProjectID != "" {
				t.Fatalf("late completion changed terminal status: %+v %v", final, err)
			}
			encoded, err := json.Marshal(status)
			if err != nil || strings.Contains(string(encoded), "private-token") {
				t.Fatalf("secret escaped terminal state: %s %v", encoded, err)
			}
			if _, err := service.SelectLoginProject(flow.FlowID, "project"); err == nil {
				t.Fatal("terminal login allowed another provisioning attempt")
			}
		})
	}
}

func TestProviderCompletionPersistsCredentialsOnlyBeforeCancellation(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "persist")
	defer service.Close()
	auth := inferencenet.Auth{UserEmail: "owner@example.test", TeamName: "Test workspace", ProjectID: "project", ProjectName: "Project", MachineKey: "machine-secret", MachineKeyName: "Host"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.finish(ctx, auth); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled completion=%v", err)
	}
	before, err := inferencenet.LoadAuth()
	if err != nil || before.MachineKey != "" {
		t.Fatalf("cancelled completion persisted credentials: %+v %v", before, err)
	}
	if err := service.finish(t.Context(), inferencenet.Auth{}); err == nil {
		t.Fatal("completion without project credentials accepted")
	}
	if err := service.finish(t.Context(), auth); err != nil {
		t.Fatal(err)
	}
	loaded, err := inferencenet.LoadAuth()
	if err != nil || loaded.MachineKey != auth.MachineKey || loaded.ProjectID != auth.ProjectID || loaded.TeamName != auth.TeamName {
		t.Fatalf("completion did not persist host credentials: %v", err)
	}
	status, err := service.ProviderStatus(config.InferenceNetProvider)
	if err != nil || !status.Configured || status.Email != auth.UserEmail || status.TeamName != auth.TeamName || status.KeySource != "machine" || status.MachineKeyName != "Host" {
		t.Fatalf("safe account identity=%+v %v", status, err)
	}
	beforeConfig, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	validationCtx, stopValidation := context.WithCancel(t.Context())
	defer stopValidation()
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		stopValidation()
		return []llm.ModelInfo{{ID: "valid"}}, nil
	}
	if _, err := service.SetProviderKey(validationCtx, protocol.ProviderKeySetup{Revision: beforeConfig.Revision, Provider: "openrouter", Key: "replacement-secret"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled key setup=%v", err)
	}
	afterConfig, err := service.ReadConfiguration()
	if err != nil || !reflect.DeepEqual(afterConfig, beforeConfig) {
		t.Fatalf("cancelled key setup changed config: %+v %v", afterConfig, err)
	}
}
