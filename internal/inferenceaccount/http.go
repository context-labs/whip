package inferenceaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	controlURL       = "https://observability-api.inference.net"
	dashboardURL     = "https://inference.net"
	maxResponseBytes = 1 << 20
)

type remoteError struct{ uncertain bool }

func (e *remoteError) Error() string {
	if e.uncertain {
		return "Inference.net operation may have completed; inspect the account before starting another operation"
	}
	return "Inference.net account request failed"
}

// request never retries or exposes response/transport diagnostics. Mutating
// responses that do not establish an outcome remain explicitly uncertain.
func (s *Service) request(ctx context.Context, method, path, token, team string, input, output any, mutation bool) (int, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return 0, ErrInvalid
		}
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, controlURL+path, body)
	if err != nil {
		return 0, ErrInvalid
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Origin", dashboardURL)
	}
	if team != "" {
		request.Header.Set("X-Inference-Team-Id", team)
	}
	response, err := s.http.Do(request)
	if err != nil {
		return 0, &remoteError{uncertain: mutation}
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return response.StatusCode, &remoteError{uncertain: mutation}
	}
	if output != nil && json.Unmarshal(raw, output) != nil {
		return response.StatusCode, &remoteError{uncertain: mutation}
	}
	return response.StatusCode, nil
}

func (s *Service) call(ctx context.Context, method, path, token, team string, input, output any, mutation bool) error {
	status, err := s.request(ctx, method, path, token, team, input, output, mutation)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &remoteError{uncertain: mutation && status >= 500}
	}
	return nil
}

func (s *Service) activate(ctx context.Context, token, team string) error {
	return s.call(ctx, http.MethodPost, "/api/auth/organization/set-active", token, "", map[string]string{"organizationId": team}, nil, false)
}

func (s *Service) archive(ctx context.Context, token, team, key string) error {
	if key == "" {
		return nil
	}
	if token == "" {
		return ErrManagement
	}
	if !text(key, 256, false) || key == "." || key == ".." || strings.ContainsAny(key, "/\\") || !text(team, 256, false) {
		return ErrInvalid
	}
	if err := s.activate(ctx, token, team); err != nil {
		return err
	}
	status, err := s.request(ctx, http.MethodDelete, "/api/rest/api-keys/"+url.PathEscape(key)+"?teamId="+url.QueryEscape(team), token, team, nil, nil, false)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return &remoteError{}
	}
	return nil
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func uncertain(err error) bool {
	var remote *remoteError
	return errors.As(err, &remote) && remote.uncertain
}
