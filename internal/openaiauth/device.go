package openaiauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Public client and device endpoints used by OpenAI Codex's device_code_auth.rs.
// See the pinned upstream references in .ai-docs/plans/openai-subscriptions.
const (
	clientID       = "app_EMoamEEZ73f0CkXaXp7hrann"
	issuer         = "https://auth.openai.com"
	DeviceLifetime = 15 * time.Minute
)

// DeviceCode contains the public verification details and private polling state.
// Only the URL, user code, and expiry may be shown to attached clients.
type DeviceCode struct {
	VerificationURL string
	UserCode        string
	ExpiresAt       time.Time
	deviceID        string
	interval        time.Duration
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type authError struct {
	status  int
	code    string
	pending bool
}

func (e *authError) Error() string {
	switch e.code {
	case "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated", "invalid_grant":
		return "OpenAI login expired or was revoked; sign in again"
	case "access_denied":
		return "OpenAI sign-in was denied"
	}
	return fmt.Sprintf("OpenAI authentication request failed (HTTP %d)", e.status)
}

func (e *authError) requiresLogin() bool {
	return e.status == http.StatusUnauthorized || e.code == "refresh_token_expired" ||
		e.code == "refresh_token_reused" || e.code == "refresh_token_invalidated" || e.code == "invalid_grant"
}

func (m *Manager) StartDevice(ctx context.Context) (DeviceCode, error) {
	var response struct {
		DeviceID string          `json:"device_auth_id"`
		UserCode string          `json:"user_code"`
		Interval json.RawMessage `json:"interval"`
	}
	err := m.postJSON(ctx, "/api/accounts/deviceauth/usercode", map[string]string{"client_id": clientID}, &response)
	if err != nil {
		var denied *authError
		if errors.As(err, &denied) && denied.status == http.StatusForbidden {
			return DeviceCode{}, errors.New("OpenAI device login is unavailable; enable device code authorization for Codex in ChatGPT Security settings, then retry")
		}
		return DeviceCode{}, err
	}
	if !headerValue(response.DeviceID) || !headerValue(response.UserCode) || len(response.UserCode) > 64 {
		return DeviceCode{}, errors.New("OpenAI returned an invalid device code")
	}
	interval := 5
	if len(response.Interval) > 0 {
		value := strings.Trim(string(response.Interval), `"`)
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 60 {
			return DeviceCode{}, errors.New("OpenAI returned an invalid device polling interval")
		}
		interval = parsed
	}
	return DeviceCode{
		VerificationURL: issuer + "/codex/device", UserCode: response.UserCode,
		ExpiresAt: time.Now().Add(DeviceLifetime), deviceID: response.DeviceID,
		interval: time.Duration(interval) * time.Second,
	}, nil
}

func (m *Manager) CompleteDevice(ctx context.Context, code DeviceCode) (Credentials, error) {
	if code.deviceID == "" || code.interval <= 0 || code.ExpiresAt.IsZero() {
		return Credentials{}, errors.New("invalid OpenAI device login")
	}
	ctx, cancel := context.WithDeadline(ctx, code.ExpiresAt)
	defer cancel()
	for {
		timer := time.NewTimer(code.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Credentials{}, ctx.Err()
		case <-timer.C:
		}
		var response struct {
			Code     string `json:"authorization_code"`
			Verifier string `json:"code_verifier"`
		}
		err := m.postJSON(ctx, "/api/accounts/deviceauth/token", map[string]string{
			"device_auth_id": code.deviceID, "user_code": code.UserCode,
		}, &response)
		var pending *authError
		if errors.As(err, &pending) && pending.pending {
			continue
		}
		if err != nil {
			return Credentials{}, err
		}
		if response.Code == "" || response.Verifier == "" {
			return Credentials{}, errors.New("OpenAI returned an invalid device authorization")
		}
		return m.exchange(ctx, url.Values{
			"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {response.Code},
			"code_verifier": {response.Verifier}, "redirect_uri": {issuer + "/deviceauth/callback"},
		}, Credentials{})
	}
}

func (m *Manager) postJSON(ctx context.Context, path string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return errors.New("could not encode OpenAI authentication request")
	}
	return m.post(ctx, path, "application/json", bytes.NewReader(data), output)
}

func (m *Manager) post(ctx context.Context, path, contentType string, body io.Reader, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.issuer+path, body)
	if err != nil {
		return errors.New("could not build OpenAI authentication request")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "whip")
	response, err := m.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("could not reach OpenAI authentication service; retry sign-in")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return errors.New("could not read bounded OpenAI authentication response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error json.RawMessage `json:"error"`
			Code  string          `json:"code"`
		}
		_ = json.Unmarshal(data, &failure)
		var errorCode string
		if json.Unmarshal(failure.Error, &errorCode) != nil {
			var nested struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(failure.Error, &nested)
			errorCode = nested.Code
		}
		if errorCode == "" {
			errorCode = failure.Code
		}
		return &authError{
			status: response.StatusCode, code: errorCode,
			pending: path == "/api/accounts/deviceauth/token" &&
				(response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound),
		}
	}
	if json.Unmarshal(data, output) != nil {
		return errors.New("OpenAI returned malformed authentication data")
	}
	return nil
}

func (m *Manager) exchange(ctx context.Context, form url.Values, previous Credentials) (Credentials, error) {
	var response tokenResponse
	if err := m.post(ctx, "/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()), &response); err != nil {
		return Credentials{}, err
	}
	if response.ExpiresIn == 0 {
		// The Codex token exchange may omit expiry; OpenCode uses the same
		// conservative one-hour refresh interval for that response shape.
		response.ExpiresIn = 3600
	}
	if response.ExpiresIn < 0 || response.ExpiresIn > 366*24*60*60 {
		return Credentials{}, errors.New("OpenAI returned an invalid token expiry")
	}
	credentials := previous
	credentials.AccessToken = response.AccessToken
	credentials.ExpiresAt = time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)
	if response.RefreshToken != "" {
		credentials.RefreshToken = response.RefreshToken
	}
	access, id := tokenClaims(response.AccessToken), tokenClaims(response.IDToken)
	accountID := claimString(access, "chatgpt_account_id")
	if accountID == "" {
		accountID = claimString(id, "chatgpt_account_id")
	}
	if previous.AccountID != "" && accountID != "" && previous.AccountID != accountID {
		return Credentials{}, errors.New("OpenAI refreshed a different account; sign in again")
	}
	if accountID != "" {
		credentials.AccountID = accountID
	}
	if email, ok := id["email"].(string); ok {
		credentials.Email = email
	}
	if plan := claimString(access, "chatgpt_plan_type"); plan != "" {
		credentials.Plan = plan
	}
	credentials.ComputeResidency = claimString(access, "chatgpt_compute_residency")
	if credentials.ComputeResidency == "no_constraint" {
		credentials.ComputeResidency = ""
	}
	return credentials, credentials.validate()
}

// JWT claims are display/routing metadata from the HTTPS token exchange, not
// a locally verified identity or a substitute for upstream authorization.
func tokenClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	_ = json.Unmarshal(data, &claims)
	return claims
}

func claimString(claims map[string]any, key string) string {
	if value, ok := claims[key].(string); ok {
		return value
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		value, _ := auth[key].(string)
		return value
	}
	return ""
}
