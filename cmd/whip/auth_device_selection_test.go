package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
)

func TestDeviceLoginChoosesWorkspaceAndCreatesProject(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	useTestDaemon(t)
	mux := http.NewServeMux()
	for path, body := range map[string]string{
		"/api/auth/device/code":       `{"device_code":"device","user_code":"CODE","expires_in":30,"interval":1}`,
		"/api/auth/device/token":      `{"access_token":"session-token"}`,
		"/api/auth/get-session":       `{"user":{"email":"dev@example.com","id":"user-1"}}`,
		"/api/auth/organization/list": `[{"id":"team-1","name":"Work"},{"id":"team-2","name":"Work"}]`,
		"/api/rest/projects":          `[{"id":"project-1","name":"First"},{"id":"project-2","name":"Second"}]`,
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
	}
	mux.HandleFunc("/api/auth/organization/set-active", func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["organizationId"] != "team-2" {
			t.Errorf("wrong workspace selected: %v %v", input, err)
		}
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/api/rest/projects/create", func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["name"] != "Created from CLI" {
			t.Errorf("wrong project name: %v %v", input, err)
		}
		_, _ = io.WriteString(w, `{"id":"new-project","name":"Created from CLI"}`)
	})
	mux.HandleFunc("/api/rest/api-keys", func(w http.ResponseWriter, r *http.Request) {
		var input struct{ TeamID, DefaultProjectID string }
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.TeamID != "team-2" || input.DefaultProjectID != "new-project" {
			t.Errorf("machine key received wrong scope: %+v %v", input, err)
		}
		_, _ = io.WriteString(w, `{"id":"key-1","key":"machine-secret"}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	defer inferencenet.SetURLsForTest(server.URL, server.URL, server.URL)()

	output := answerDeviceLoginPrompts(t, func() error {
		return providerDeviceLogin(config.InferenceNetProvider, 10*time.Second)
	})
	if !strings.Contains(output, "Signed in as dev@example.com") || !strings.Contains(output, "Project new-project") {
		t.Fatalf("login did not report the new project: %s", output)
	}
	stored, err := inferencenet.LoadAuth()
	if err != nil || stored.TeamID != "team-2" || stored.ProjectID != "new-project" || stored.ProjectName != "Created from CLI" || stored.MachineKey != "machine-secret" {
		t.Fatalf("login did not persist selected workspace and new project: %+v %v", stored, err)
	}
}

// Answer after each visible prompt, as a terminal user would, without timing sleeps.
func answerDeviceLoginPrompts(t *testing.T, login func() error) string {
	t.Helper()
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inputR.Close(); _ = inputW.Close() }()
	if err := inputR.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	outputR, outputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outputR.Close(); _ = outputW.Close() }()
	previousIn, previousOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inputR, outputW
	defer func() { os.Stdin, os.Stdout = previousIn, previousOut }()
	answers := []struct{ prompt, answer string }{
		{"pick [1-2, default 1]: ", "2\n"},
		{"pick [1-3, default 1]: ", "3\n"},
		{"  name: ", "Created from CLI\n"},
	}
	finished := make(chan string, 1)
	go func() {
		var output strings.Builder
		reader := bufio.NewReader(outputR)
		next := 0
		for {
			b, readErr := reader.ReadByte()
			if readErr != nil {
				break
			}
			output.WriteByte(b)
			if next < len(answers) && strings.HasSuffix(output.String(), answers[next].prompt) {
				if _, writeErr := io.WriteString(inputW, answers[next].answer); writeErr != nil {
					fmt.Fprintf(&output, "input write failed: %v", writeErr)
					break
				}
				next++
			}
		}
		finished <- output.String()
	}()
	loginErr := login()
	_ = outputW.Close()
	output := <-finished
	if loginErr != nil {
		t.Fatalf("device login: %v\n%s", loginErr, output)
	}
	return output
}
