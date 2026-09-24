package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func pickerFixture() protocol.ProviderList {
	list := protocol.ProviderList{Revision: "fixture", Selection: &protocol.ProviderSelection{Reason: "provider_unavailable"}}
	for _, preset := range config.ProviderPresets() {
		list.Providers = append(list.Providers, protocol.ProviderEntry{
			ID: preset.ID, Name: preset.Provider.Name, Methods: preset.Methods, Category: preset.Category,
			Family: preset.Family, KeyURL: preset.KeyURL, Recommended: preset.Recommended,
			Status: protocol.ProviderStatus{Available: new(false)},
		})
	}
	return list
}

func pickerEntry(t *testing.T, list *protocol.ProviderList, id string) *protocol.ProviderEntry {
	t.Helper()
	for i := range list.Providers {
		if list.Providers[i].ID == id {
			return &list.Providers[i]
		}
	}
	t.Fatalf("missing fixture provider %s", id)
	return nil
}

func TestPickerGroupsFamiliesAndMarksConnectionsWithoutMovingThem(t *testing.T) {
	h := &setupTestHost{list: pickerFixture()}
	inference := pickerEntry(t, &h.list, "inference-net")
	inference.Status.Configured, inference.Status.AuthState = true, "key_required"
	groq := pickerEntry(t, &h.list, "groq")
	groq.Status = protocol.ProviderStatus{Available: new(true), KeySource: "key_file", CredentialPath: "/fixture/GROQ_API_KEY"}
	pickerEntry(t, &h.list, "openai-codex").Status.Available = new(true)
	pickerEntry(t, &h.list, "deepseek").Status.Configured = true
	pickerEntry(t, &h.list, "deepseek").Status.AuthState = "configuration_error"
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	entries := s.entries()
	want := []string{"inference-net", "openrouter", "openai", "cerebras", "deepinfra", "deepseek", "fireworks-ai", "groq", "togetherai", "xai"}
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	if !slices.Equal(ids, want) || s.selected != 0 || s.mode != "providers" {
		t.Fatalf("unstable initial inventory: ids=%v, selection=%d, mode=%s", ids, s.selected, s.mode)
	}
	view := ansi.Strip(s.body())
	for _, text := range []string{"Popular", "Providers", "✓ OpenAI", "✓ Groq", "! DeepSeek", "Custom endpoint"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %q in picker:\n%s", text, view)
		}
	}
	if strings.Count(view, "Recommended") != 1 || strings.Contains(view, "> Search") || strings.Contains(view, "another provider") || strings.Contains(view, "! Inference.net") {
		t.Fatalf("unexpected picker chrome:\n%s", view)
	}
	s.input.SetValue("groq")
	if !strings.Contains(setupProviderDescription(s.entries()[0]), "Key file on this host") {
		t.Fatal("external credential provenance is missing")
	}
	s.input.SetValue("chatgpt")
	if len(s.entries()) != 1 || s.entries()[0].Family != "openai" {
		t.Fatal("subscription alias is not searchable")
	}
	s.input.SetValue("popular")
	if len(s.entries()) != 3 {
		t.Fatal("provider categories are not searchable")
	}
}

func TestPickerRefreshPreservesSelectedProviderAndNeverAutoConnects(t *testing.T) {
	h := &setupTestHost{list: pickerFixture()}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.selected = 7 // Groq, in display order.
	pickerEntry(t, &h.list, "groq").Status.Available = new(true)
	applySetupCommand(t, s, s.refresh(""))
	if s.entries()[s.selected].ID != "groq" || s.mode != "providers" || s.chosen || len(h.began) > 0 || h.catalogProvider != "" {
		t.Fatal("credential discovery moved selection or initiated a connection")
	}
	s.input.SetValue("no matches")
	s.selected = 0
	applySetupCommand(t, s, s.refresh(""))
	if s.selected != len(s.entries()) {
		t.Fatal("Other selection lost after refresh")
	}
}

func TestPickerKnownPresetOnlyAsksForKey(t *testing.T) {
	for _, id := range []string{"openrouter", "cerebras", "groq", "deepseek", "fireworks-ai", "togetherai", "deepinfra", "xai"} {
		t.Run(id, func(t *testing.T) {
			h := &setupTestHost{list: pickerFixture()}
			s := newTestSetup(t, h, true)
			applySetupCommand(t, s, s.Init())
			s.Update(tea.PasteMsg{Content: id})
			s.keypress(setupKey("enter"))
			if s.mode != "key" || s.provider != id || s.form != nil || len(h.began) > 0 {
				t.Fatalf("preset did not open key-only flow: mode=%s, provider=%s", s.mode, s.provider)
			}
			s.Update(tea.PasteMsg{Content: "fixture-private-key"})
			view := ansi.Strip(s.body())
			for _, forbidden := range []string{"fixture-private-key", "Base URL", "Advanced", "Enter a model manually", "> ", "ctrl+o", s.entry().Name} {
				if strings.Contains(view, forbidden) {
					t.Fatalf("key prompt contains %q", forbidden)
				}
			}
			next := applySetupCommand(t, s, s.keypress(setupKey("enter")))
			providerFormDrain(t, s, next)
			if h.key != "fixture-private-key" || s.input.Value() != "" || s.provider != id || s.mode != "models" || s.done || len(h.writes) != 0 {
				t.Fatal("key save did not offer models for its selected provider")
			}
		})
	}
}

func TestPickerOpenAIMethodsKeepRealProviderIdentity(t *testing.T) {
	for _, connected := range []string{"", "openai", "openai-codex", "both"} {
		t.Run(connected, func(t *testing.T) {
			h := &setupTestHost{list: pickerFixture()}
			for _, id := range []string{"openai", "openai-codex"} {
				entry := pickerEntry(t, &h.list, id)
				entry.Status.Available = new(connected == id || connected == "both")
				entry.SuggestedModel = "model-for-" + id
			}
			s := newTestSetup(t, h, false)
			applySetupCommand(t, s, s.Init())
			s.input.SetValue("OpenAI")
			providerFormDrain(t, s, s.keypress(setupKey("enter")))
			if connected == "openai" || connected == "openai-codex" {
				if s.mode != "models" || s.provider != connected || s.model != "model-for-"+connected {
					t.Fatal("single connected family member was not used directly")
				}
			} else {
				if s.mode != "methods" || len(s.methods) != 2 {
					t.Fatal("API/subscription selection is missing")
				}
				s.selected = 0
				providerFormDrain(t, s, s.keypress(setupKey("enter")))
				if s.provider != "openai" || connected == "" && s.mode != "key" || connected == "both" && s.mode != "models" {
					t.Fatal("API selection did not retain the real API provider ID")
				}
			}
			s = newTestSetup(t, h, false)
			applySetupCommand(t, s, s.Init())
			s.input.SetValue("OpenAI")
			s.keypress(providerFormKey("ctrl+e"))
			if s.mode != "methods" || !s.manageMethods || len(s.methods) != 2 {
				t.Fatal("Manage hid an API/subscription connection")
			}
			if len(h.writes) != 0 || h.key != "" || len(h.began) > 0 {
				t.Fatal("opening or managing a family mutated a route")
			}
		})
	}
}

func TestPickerMultiMethodSelectionDoesNotStartBrowserUntilChosen(t *testing.T) {
	h := &setupTestHost{list: pickerFixture()}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.keypress(setupKey("enter"))
	if s.mode != "methods" || len(h.began) != 0 {
		t.Fatal("opening Inference.net started browser login without choosing a method")
	}
	s.keypress(setupKey("down"))
	s.keypress(setupKey("enter"))
	if s.mode != "key" || s.provider != "inference-net" || len(h.began) != 0 {
		t.Fatal("API-key method began browser login")
	}
	s.input.SetValue("do-not-show-this")
	applySetupCommand(t, s, s.keypress(providerFormKey("ctrl+r")))
	if s.input.Value() != "" || strings.Contains(ansi.Strip(s.body()), "do-not-show-this") {
		t.Fatal("refresh moved a secret into provider search")
	}
}

func TestPickerPendingSearchClipboardCannotEnterKeyPrompt(t *testing.T) {
	h := &setupTestHost{list: pickerFixture()}
	s := newTestSetup(t, h, true)
	applySetupCommand(t, s, s.Init())
	s.input.SetValue("groq")
	pending := s.inputCommand(func() tea.Msg { return tea.PasteMsg{Content: "search-clipboard"} })
	s.keypress(setupKey("enter"))
	s.Update(pending())
	if s.mode != "key" || s.input.Value() != "" || h.key != "" {
		t.Fatal("pending search clipboard crossed into a credential prompt")
	}
}

func TestLoginDialogKeepsApprovalLinkAndCodeOnSmallTerminals(t *testing.T) {
	for _, provider := range []string{"inference-net", "openai-codex"} {
		for _, width := range []int{28, 44, 64} {
			for _, height := range []int{8, 12, 18} {
				s := newTestSetup(t, &setupTestHost{list: pickerFixture()}, true)
				s.list, s.mode, s.provider = pickerFixture(), "login", provider
				s.login.VerificationURL = "https://inference.net/device/approve?user_code=TEST1234"
				s.login.UserCode = "TEST1234"
				rows := s.loginRows(width, height)
				view := ansi.Strip(strings.Join(rows, "\n"))
				if len(rows) > height {
					t.Fatalf("%s at %dx%d: %d rows", provider, width, height, len(rows))
				}
				if !strings.Contains(view, "Code  TEST1234") || !strings.Contains(view, "esc") {
					t.Fatalf("approval code or escape hidden at %dx%d:\n%s", width, height, view)
				}
				if !strings.Contains(strings.Join(strings.Fields(view), ""), s.login.VerificationURL) {
					t.Fatalf("approval link truncated at %dx%d:\n%s", width, height, view)
				}
			}
		}
	}
}

func TestLoginFeedbackSurvivesPollingWithoutHidingErrors(t *testing.T) {
	s := newTestSetup(t, &setupTestHost{list: pickerFixture()}, true)
	s.list, s.mode, s.provider = pickerFixture(), "login", "inference-net"
	s.login.FlowID, s.login.State = "flow", "pending"
	s.loginFeedback = "Open the link above in your browser."
	s.Update(setupReply{owner: s, request: s.request, kind: "login", login: s.login})
	if !strings.Contains(ansi.Strip(s.body()), s.loginFeedback) {
		t.Fatal("sign-in polling erased browser feedback")
	}
	s.message = "Connection interrupted"
	view := ansi.Strip(s.body())
	if !strings.Contains(view, s.message) || strings.Contains(view, s.loginFeedback) {
		t.Fatal("browser feedback hid a sign-in error")
	}
}

func TestPickerPromptsPaintPaddingAndLeaveCursorRoom(t *testing.T) {
	t.Cleanup(func() { SetLightTheme(false) })
	for _, light := range []bool{false, true} {
		SetLightTheme(light)
		for _, width := range []int{28, 44, 68} {
			t.Run(fmt.Sprintf("light=%t/width=%d", light, width), func(t *testing.T) {
				s := newTestSetup(t, &setupTestHost{list: pickerFixture()}, true)
				s.list = pickerFixture()
				for _, mode := range []string{"providers", "key", "provider_form", "login", "project_name"} {
					s.mode, s.provider = mode, "groq"
					var rows []string
					switch mode {
					case "providers":
						rows = s.providerRows(width, 18)
					case "key":
						rows = s.keyRows(width, 18)
					case "login":
						s.login.VerificationURL = "https://inference.net/device/approve?user_code=TEST1234"
						s.login.UserCode = "TEST1234"
						rows = s.loginRows(width, 18)
					case "project_name":
						rows = s.projectNameRows(width, 18)
					case "provider_form":
						s.form = newSetupProviderForm(protocol.ProviderConfiguration{}, false, false)
						rows = s.providerFormRows(width, 18)
					}
					if mode != "providers" && mode != "login" && strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "…") {
						t.Fatal("empty input has overflow ellipses")
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != width {
							t.Fatalf("%s row width=%d, expected %d", mode, ansi.StringWidth(row), width)
						}
						screen := uv.NewScreenBuffer(width, 1)
						uv.NewStyledString(row).Draw(screen, screen.Bounds())
						for _, x := range []int{0, 1, width - 2, width - 1} {
							cell := screen.CellAt(x, 0)
							if cell == nil || cell.Style.Bg == nil {
								t.Fatalf("%s leaves unpainted padding at column %d", mode, x)
							}
						}
					}
				}
			})
		}
	}
}
