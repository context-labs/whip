// Package codexauth manages the local OAuth state used by Codex subscriptions.
package codexauth

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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	issuerURL          = "https://auth.openai.com"
	clientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	deviceLoginTimeout = 15 * time.Minute
)

var (
	ErrLoginRequired          = errors.New("codex authentication not found; run whip auth codex")
	ErrDeviceLoginUnsupported = errors.New("device-code login is not enabled for this Codex account")
	ErrDeviceLoginTimeout     = errors.New("device login timed out after 15 minutes")
)

// Credentials are the non-persisted fields needed for one Codex request.
// RefreshToken deliberately stays private so callers cannot accidentally log it.
type Credentials struct {
	AccessToken string
	AccountID   string
}

// Source reads and writes Whip's Codex credentials. The exported transport
// fields make the package testable without a real login.
type Source struct {
	HomeDir   string
	HTTP      *http.Client
	IssuerURL string
	TokenURL  string

	mu  sync.Mutex
	now func() time.Time
}

// DeviceCode is displayed to the user while DeviceLogin waits for approval.
// It deliberately contains no OAuth credentials.
type DeviceCode struct {
	VerificationURL string
	UserCode        string
}

// Available reports whether a usable local login exists. It does not refresh:
// startup remains local and a near-expiry token is refreshed when it is used.
func (s *Source) Available() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.load()
	return err
}

// Credentials returns current credentials, refreshing tokens that are expired
// or within five minutes of expiry. Refreshed values are persisted atomically
// because refresh tokens may rotate.
func (s *Source) Credentials(ctx context.Context) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, err := s.load()
	if err != nil {
		return Credentials{}, err
	}
	if c.access != "" && (c.expiresAt.IsZero() || c.expiresAt.After(s.clock().Add(5*time.Minute))) {
		return c.credentials(), nil
	}
	if c.refresh == "" {
		return Credentials{}, ErrLoginRequired
	}
	if err := s.refresh(ctx, c); err != nil {
		return Credentials{}, err
	}
	return c.credentials(), nil
}

type candidate struct {
	path string
	root map[string]json.RawMessage

	access, refresh, idToken, accountID string
	expiresAt                           time.Time
}

type tokenClaims struct {
	Exp  int64 `json:"exp"`
	Auth struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

func (c candidate) credentials() Credentials {
	return Credentials{AccessToken: c.access, AccountID: c.accountID}
}

func (s *Source) load() (*candidate, error) {
	c, err := s.codexCandidate()
	if err != nil {
		return nil, ErrLoginRequired
	}
	tokens, ok := c.root["tokens"]
	if !ok {
		return nil, ErrLoginRequired
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(tokens, &fields); err != nil {
		return nil, ErrLoginRequired
	}
	c.access = stringField(fields, "access_token")
	c.refresh = stringField(fields, "refresh_token")
	c.idToken = stringField(fields, "id_token")
	c.accountID = stringField(fields, "account_id")
	c.fillJWTClaims()
	if c.accountID == "" || (c.access == "" && c.refresh == "") {
		return nil, ErrLoginRequired
	}
	return c, nil
}

func (s *Source) homeDir() (string, error) {
	if s.HomeDir != "" {
		return s.HomeDir, nil
	}
	return os.UserHomeDir()
}

func (s *Source) codexCandidate() (*candidate, error) {
	home, err := s.homeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".codex", "auth.json")
	//nolint:gosec // path is constructed from the user's Codex home directory.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &candidate{path: path, root: map[string]json.RawMessage{}}, nil
	}
	if err != nil {
		return nil, err
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("auth file must contain a JSON object")
	}
	return &candidate{path: path, root: root}, nil
}

func stringField(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}

func (c *candidate) fillJWTClaims() {
	for _, token := range []string{c.access, c.idToken} {
		claims, ok := jwtClaims(token)
		if !ok {
			continue
		}
		if c.expiresAt.IsZero() && claims.Exp > 0 {
			c.expiresAt = time.Unix(claims.Exp, 0)
		}
		if c.accountID == "" {
			c.accountID = claims.Auth.ChatGPTAccountID
		}
	}
}

func jwtClaims(token string) (tokenClaims, bool) {
	var claims tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return claims, false
		}
	}
	if json.Unmarshal(payload, &claims) != nil {
		return claims, false
	}
	return claims, true
}

// DeviceLogin signs in with Codex's device-code flow. show receives the
// verification URL and transient user code before this method waits for the
// user to finish approval. It never receives OAuth credentials.
func (s *Source) DeviceLogin(ctx context.Context, show func(DeviceCode)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, err := s.codexCandidate()
	if err != nil {
		return fmt.Errorf("read Codex credentials: %w", err)
	}
	device, err := s.requestDeviceCode(ctx)
	if err != nil {
		return err
	}
	if show != nil {
		show(DeviceCode{
			VerificationURL: s.issuer() + "/codex/device",
			UserCode:        device.userCode,
		})
	}
	tokens, err := s.pollDeviceCode(ctx, device)
	if err != nil {
		return err
	}
	c.access = tokens.AccessToken
	c.refresh = tokens.RefreshToken
	c.idToken = tokens.IDToken
	c.expiresAt = expiryFromDuration(tokens.ExpiresIn, s.clock())
	c.fillJWTClaims()
	if c.accountID == "" {
		return errors.New("could not determine Codex account from device login")
	}
	if c.expiresAt.IsZero() {
		return errors.New("could not determine Codex token expiry from device login")
	}
	if err := c.save(s.clock()); err != nil {
		return fmt.Errorf("save Codex credentials: %w", err)
	}
	return nil
}

type deviceLogin struct {
	deviceAuthID string
	userCode     string
	interval     time.Duration
}

type tokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	IDToken      string          `json:"id_token"`
	ExpiresIn    json.RawMessage `json:"expires_in"`
}

func (s *Source) requestDeviceCode(ctx context.Context) (deviceLogin, error) {
	body, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: clientID})
	if err != nil {
		return deviceLogin{}, fmt.Errorf("prepare device login: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.issuer()+"/api/accounts/deviceauth/usercode", bytes.NewReader(body))
	if err != nil {
		return deviceLogin{}, fmt.Errorf("start device login: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return deviceLogin{}, fmt.Errorf("start device login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return deviceLogin{}, ErrDeviceLoginUnsupported
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return deviceLogin{}, fmt.Errorf("start device login: server returned %s", resp.Status)
	}
	var out struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.DeviceAuthID == "" || out.UserCode == "" {
		return deviceLogin{}, errors.New("start device login: invalid server response")
	}
	return deviceLogin{
		deviceAuthID: out.DeviceAuthID,
		userCode:     out.UserCode,
		interval:     pollInterval(out.Interval),
	}, nil
}

func (s *Source) pollDeviceCode(ctx context.Context, device deviceLogin) (tokenResponse, error) {
	timeout := time.NewTimer(deviceLoginTimeout)
	defer timeout.Stop()

	for {
		body, err := json.Marshal(struct {
			DeviceAuthID string `json:"device_auth_id"`
			UserCode     string `json:"user_code"`
		}{DeviceAuthID: device.deviceAuthID, UserCode: device.userCode})
		if err != nil {
			return tokenResponse{}, fmt.Errorf("poll device login: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.issuer()+"/api/accounts/deviceauth/token", bytes.NewReader(body))
		if err != nil {
			return tokenResponse{}, fmt.Errorf("poll device login: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient().Do(req)
		if err != nil {
			return tokenResponse{}, fmt.Errorf("poll device login: %w", err)
		}
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			var out struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
			_ = resp.Body.Close()
			if err != nil || out.AuthorizationCode == "" || out.CodeVerifier == "" {
				return tokenResponse{}, errors.New("poll device login: invalid server response")
			}
			return s.exchangeDeviceCode(ctx, out.AuthorizationCode, out.CodeVerifier)
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status != http.StatusForbidden && status != http.StatusNotFound {
			return tokenResponse{}, fmt.Errorf("poll device login: server returned %s", resp.Status)
		}

		timer := time.NewTimer(device.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return tokenResponse{}, ctx.Err()
		case <-timeout.C:
			if !timer.Stop() {
				<-timer.C
			}
			return tokenResponse{}, ErrDeviceLoginTimeout
		case <-timer.C:
		}
	}
}

func (s *Source) exchangeDeviceCode(ctx context.Context, code, verifier string) (tokenResponse, error) {
	form := url.Values{
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {s.issuer() + "/deviceauth/callback"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), bytes.NewBufferString(form.Encode()))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("exchange device login: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("exchange device login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return tokenResponse{}, fmt.Errorf("exchange device login: server returned %s", resp.Status)
	}
	var out tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.AccessToken == "" || out.RefreshToken == "" || out.IDToken == "" {
		return tokenResponse{}, errors.New("exchange device login: invalid server response")
	}
	return out, nil
}

func pollInterval(raw json.RawMessage) time.Duration {
	seconds := int64(1)
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if n, err := strconv.ParseInt(text, 10, 64); err == nil && n > 0 {
		seconds = n
	}
	maxSeconds := int64(deviceLoginTimeout / time.Second)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (s *Source) refresh(ctx context.Context, c *candidate) error {
	form := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refresh},
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), bytes.NewBufferString(form.Encode()))
	if err != nil {
		return fmt.Errorf("refresh codex login: %w", err)
	}
	hr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient().Do(hr)
	if err != nil {
		return errors.New("could not refresh codex login; run whip auth codex")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return errors.New("could not refresh codex login; run whip auth codex")
	}
	var out tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.AccessToken == "" {
		return errors.New("could not refresh codex login; run whip auth codex")
	}
	c.access = out.AccessToken
	if out.RefreshToken != "" {
		c.refresh = out.RefreshToken
	}
	if out.IDToken != "" {
		c.idToken = out.IDToken
	}
	c.expiresAt = expiryFromDuration(out.ExpiresIn, s.clock())
	c.fillJWTClaims()
	if c.accountID == "" {
		return errors.New("could not determine codex account; run whip auth codex")
	}
	if c.expiresAt.IsZero() {
		return errors.New("could not determine codex token expiry; run whip auth codex")
	}
	if err := c.save(s.clock()); err != nil {
		return fmt.Errorf("save refreshed codex login: %w", err)
	}
	return nil
}

func expiryFromDuration(raw json.RawMessage, now time.Time) time.Time {
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if seconds, err := strconv.ParseInt(n.String(), 10, 64); err == nil && seconds > 0 {
			return now.Add(time.Duration(seconds) * time.Second)
		}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if seconds, err := strconv.ParseInt(text, 10, 64); err == nil && seconds > 0 {
			return now.Add(time.Duration(seconds) * time.Second)
		}
	}
	return time.Time{}
}

func (c *candidate) save(now time.Time) error {
	fields := map[string]json.RawMessage{}
	if raw, ok := c.root["tokens"]; ok {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
	}
	fields["access_token"] = marshalRaw(c.access)
	fields["refresh_token"] = marshalRaw(c.refresh)
	if c.idToken != "" {
		fields["id_token"] = marshalRaw(c.idToken)
	}
	fields["account_id"] = marshalRaw(c.accountID)
	c.root["tokens"] = marshalRaw(fields)
	c.root["auth_mode"] = marshalRaw("chatgpt")
	c.root["last_refresh"] = marshalRaw(now.UTC().Format(time.RFC3339))
	data, err := json.MarshalIndent(c.root, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(c.path, append(data, '\n'))
}

func marshalRaw(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".whip-codex-auth-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *Source) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Source) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *Source) tokenURL() string {
	if s.TokenURL != "" {
		return s.TokenURL
	}
	return s.issuer() + "/oauth/token"
}

func (s *Source) issuer() string {
	if s.IssuerURL != "" {
		return strings.TrimRight(s.IssuerURL, "/")
	}
	return issuerURL
}
