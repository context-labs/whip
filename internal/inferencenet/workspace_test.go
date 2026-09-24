package inferencenet

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWorkspaceSelectionUsesChosenTeamAndProject(t *testing.T) {
	for _, test := range []struct {
		name, selectedProject, wantName string
		projects                        []Project
		interactive                     bool
	}{
		{"existing", "Second", "Second", []Project{{ID: "p1", Name: "First"}, {ID: "p2", Name: "Second"}}, true},
		{"empty selection", "", "First", []Project{{ID: "p1", Name: "First"}}, true},
		{"create default", "", "default", nil, true},
		{"automatic default", "", "default", nil, false},
		{"named project", CreateProjectOption, "Work", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			created := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/organization/set-active" {
					_, _ = w.Write([]byte("{}"))
					return
				}
				wantTeam := "first"
				if test.interactive {
					wantTeam = "chosen"
				}
				if r.Header.Get("X-Inference-Team-Id") != wantTeam || r.Header.Get("Authorization") != "Bearer session" {
					t.Errorf("workspace request has wrong identity: %s %s", r.Header.Get("X-Inference-Team-Id"), r.Header.Get("Authorization"))
				}
				switch r.URL.Path {
				case "/api/rest/projects":
					_ = json.NewEncoder(w).Encode(test.projects)
				case "/api/rest/projects/create":
					created++
					var body struct {
						Name string `json:"name"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Name != test.wantName {
						t.Errorf("created project %q, want %q", body.Name, test.wantName)
					}
					_ = json.NewEncoder(w).Encode(Project{ID: "created", Name: body.Name})
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			useStub(t, server)
			auth := Auth{SessionToken: "session"}
			sess := session{Teams: []Team{{ID: "first", Name: "Personal"}, {ID: "chosen", Name: "Company", Slug: "company"}}}
			var choose ChooseFunc
			if test.interactive {
				choose = func(kind, _ string, options []string) (string, error) {
					switch kind {
					case "team":
						if !reflect.DeepEqual(options, []string{"Personal", "Company (company)"}) {
							t.Errorf("team labels: %v", options)
						}
						return "Company (company)", nil
					case "project":
						return test.selectedProject, nil
					case "project-name":
						if test.wantName == "default" {
							return "", nil
						}
						return test.wantName, nil
					default:
						t.Fatalf("unexpected chooser %s", kind)
						return "", nil
					}
				}
			}
			if err := auth.pickWorkspace(t.Context(), sess, choose); err != nil {
				t.Fatal(err)
			}
			if auth.ProjectName != test.wantName || auth.ProjectID == "" {
				t.Fatalf("wrong project selected: %+v", auth)
			}
			if (created > 0) != (len(test.projects) == 0) {
				t.Fatalf("unexpected project mutation: %d", created)
			}
		})
	}
}

func TestWorkspaceSelectionPropagatesCancellationWithoutCreatingProject(t *testing.T) {
	for _, stage := range []string{"team", "project", "project-name"} {
		t.Run(stage, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth/organization/set-active" {
					_, _ = w.Write([]byte("{}"))
					return
				}
				if r.URL.Path != "/api/rest/projects" {
					t.Errorf("cancelled picker mutated workspace: %s", r.URL)
				}
				_, _ = w.Write([]byte("[]"))
			}))
			defer server.Close()
			useStub(t, server)
			cancelled := errors.New("picker cancelled")
			auth := Auth{SessionToken: "session"}
			err := auth.pickWorkspace(t.Context(), session{Teams: []Team{{ID: "a"}, {ID: "b"}}}, func(kind, _ string, _ []string) (string, error) {
				if kind == stage {
					return "", cancelled
				}
				return "", nil
			})
			if !errors.Is(err, cancelled) || auth.ProjectID != "" {
				t.Fatalf("cancelled selection committed: %+v, %v", auth, err)
			}
		})
	}
}
