package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

func TestAuthInferenceNetDispatch(t *testing.T) {
	m := authTestModel(t)
	m.authCommand([]string{"inference"})
	if !strings.Contains(m.transcriptText(), "sign-in") {
		t.Fatal("bare auth did not start device login")
	}
}

func TestProviderCompletionDoesNotWriteClientConfiguration(t *testing.T) {
	for _, provider := range []string{"inference-net", "openrouter", "device-login"} {
		t.Run(provider, func(t *testing.T) {
			m := authTestModel(t)
			before := m.cfg.Snapshot()
			switch provider {
			case "inference-net":
				m.applyInferenceNetKey(inferenceNetKeyMsg{})
			case "openrouter":
				m.applyAuthResult(authResultMsg{})
			case "device-login":
				m.infAuth = &inferenceNetPending{flowID: "flow"}
				if !m.applyInferenceNetLogin(inferenceNetLoginMsg{status: daemon.ProviderLoginStatus{FlowID: "flow", State: "succeeded", Email: "person@example.com"}}) {
					t.Fatal("completion not reported")
				}
			}
			after, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(before, m.cfg) {
				t.Fatal("client authentication modified configuration")
			}
			if !strings.Contains(m.transcriptText(), "execution host") {
				t.Fatal("missing host-side confirmation")
			}
		})
	}
}

func TestProviderLoginIgnoresStaleFlowAndSurfacesInterruption(t *testing.T) {
	m := authTestModel(t)
	m.infAuth = &inferenceNetPending{flowID: "new"}
	if m.applyInferenceNetLogin(inferenceNetLoginMsg{status: daemon.ProviderLoginStatus{FlowID: "old", State: "succeeded"}}) {
		t.Fatal("stale login changed active flow")
	}
	if m.infAuth == nil {
		t.Fatal("stale login cleared current flow")
	}
	m.applyInferenceNetLogin(inferenceNetLoginMsg{status: daemon.ProviderLoginStatus{FlowID: "new", State: "interrupted"}})
	if m.infAuth != nil || !strings.Contains(m.transcriptText(), "interrupted") {
		t.Fatal("interruption not surfaced")
	}
}

func TestProviderLoginError(t *testing.T) {
	m := authTestModel(t)
	m.applyInferenceNetLogin(inferenceNetLoginMsg{err: errors.New("denied")})
	if !strings.Contains(m.transcriptText(), "sign-in failed") {
		t.Fatal("error not surfaced")
	}
}

func TestProviderChoicesDisambiguateDuplicateNames(t *testing.T) {
	labels := providerChoiceLabels([]daemon.ProviderChoice{{ID: "one", Name: "Project"}, {ID: "two", Name: "Project"}})
	if labels[0] == labels[1] {
		t.Fatal("duplicate project names cannot be selected independently")
	}
}
