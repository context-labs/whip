package inferencenet

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// The relay's public REST surface (trpc-to-openapi, INF-4438) reuses the same
// session/API-key auth as tRPC but speaks plain JSON — no superjson envelope.
// These helpers wrap the few operations whipcode needs: project list/create and
// API-key create/archive.

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// restDo issues a REST call against the relay. out may be nil.
func restDo(ctx context.Context, method, token, teamID, path string, body, out any) error {
	var headers map[string]string
	if token != "" {
		headers = map[string]string{"Authorization": "Bearer " + token}
	}
	if teamID != "" {
		if headers == nil {
			headers = map[string]string{}
		}
		headers[teamIDHeader] = teamID
	}
	status, err := doJSONStatus(ctx, method, relayURL+restEndpoint+path, body, headers, out)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s failed (HTTP %d)", method, path, status)
	}
	return nil
}

// getProjects lists the active team's projects.
func getProjects(ctx context.Context, token, teamID string) ([]Project, error) {
	var projects []Project
	err := restDo(ctx, http.MethodGet, token, teamID, "/projects", nil, &projects)
	return projects, err
}

// createProject makes a new project (name only) and returns it.
func createProject(ctx context.Context, token, teamID, name string) (Project, error) {
	var p Project
	err := restDo(ctx, http.MethodPost, token, teamID, "/projects/create",
		map[string]any{"name": name}, &p)
	return p, err
}

// createAPIKey mints a key scoped to projectID, returning id + plaintext key.
func createAPIKey(ctx context.Context, token, teamID, projectID, name string) (id, key string, err error) {
	input := map[string]any{
		"defaultProjectId": projectID,
		"name":             name,
		"teamId":           teamID,
		"scopes": []map[string]any{
			{"permissions": []string{"read", "write"}, "projectId": projectID},
		},
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := restDo(ctx, http.MethodPost, token, teamID, "/api-keys", input, &created); err != nil {
		return "", "", err
	}
	return created.ID, created.Key, nil
}

// archiveAPIKey disables a key (logout / rotation). teamId is a query param.
func archiveAPIKey(ctx context.Context, token, teamID, apiKeyID string) error {
	return restDo(ctx, http.MethodDelete, token, teamID,
		"/api-keys/"+apiKeyID+"?teamId="+url.QueryEscape(teamID), nil, nil)
}
