package inferencenet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
)

var errDeviceCodeExpired = errors.New(buildinfo.Text("the device code expired; run `whip auth inference-net login` again"))

// deviceCodeResponse is the relay's /api/auth/device/code reply.
type deviceCodeResponse struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

// Team is one of the user's workspaces (organizations).
type Team struct {
	ID   string
	Name string
	Slug string
}

// session is the outcome of a completed device login: the identity plus the
// session token the control-plane calls authorize with, and the workspaces
// the user can pick from.
type session struct {
	Token  string
	Email  string
	UserID string
	Teams  []Team
}

// Login runs the OAuth device-authorization flow: request a device code,
// report the approval URL/code via onCode (the CLI opens the browser there),
// and poll until the user approves, denies, or the code expires. It resolves
// the signed-in identity and lists the workspaces for the caller to choose.
func Login(ctx context.Context, onCode func(verificationURL, userCode string)) (session, error) {
	code, err := requestDeviceCode(ctx)
	if err != nil {
		return session{}, err
	}
	verificationURL := dashboardURL + "/device/approve?user_code=" + code.UserCode
	if onCode != nil {
		onCode(verificationURL, code.UserCode)
	}
	token, err := pollForDeviceToken(ctx, code)
	if err != nil {
		return session{}, err
	}
	identity, err := getSessionIdentity(ctx, token)
	if err != nil {
		return session{}, err
	}
	teams, err := listOrganizations(ctx, token)
	if err != nil {
		return session{}, err
	}
	if len(teams) == 0 {
		return session{}, errors.New("no workspace was found for this account; finish signup in the dashboard, then try again")
	}
	return session{Token: token, Email: identity.Email, UserID: identity.UserID, Teams: teams}, nil
}

func requestDeviceCode(ctx context.Context) (deviceCodeResponse, error) {
	var out deviceCodeResponse
	err := doJSON(ctx, http.MethodPost, relayURL+"/api/auth/device/code",
		map[string]string{"client_id": DeviceClientID}, nil, &out)
	return out, err
}

func pollForDeviceToken(ctx context.Context, code deviceCodeResponse) (string, error) {
	interval := code.Interval
	if interval <= 0 {
		interval = defaultPollIntervalSeconds
	}
	if pollSleepOverride != nil { // tests collapse the wait
		interval = 0
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		if err := sleepCtx(ctx, time.Duration(interval)*time.Second); err != nil {
			return "", err
		}
		var resp struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Message     string `json:"message"`
		}
		status, err := doJSONStatus(ctx, http.MethodPost, relayURL+"/api/auth/device/token",
			map[string]string{
				"client_id":   DeviceClientID,
				"device_code": code.DeviceCode,
				"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			}, nil, &resp)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK && resp.AccessToken != "" {
			return resp.AccessToken, nil
		}
		switch resp.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
		case "access_denied":
			return "", errors.New("terminal authorization was denied")
		case "expired_token":
			return "", errDeviceCodeExpired
		default:
			msg := resp.Message
			if msg == "" {
				msg = resp.Error
			}
			if msg == "" {
				msg = fmt.Sprintf("authorization failed (HTTP %d)", status)
			}
			return "", errors.New(msg)
		}
	}
	return "", errDeviceCodeExpired
}

func getSessionIdentity(ctx context.Context, token string) (struct {
	Email  string
	UserID string
}, error,
) {
	var resp struct {
		User struct {
			Email string `json:"email"`
			ID    string `json:"id"`
		} `json:"user"`
	}
	err := doJSON(ctx, http.MethodGet, relayURL+"/api/auth/get-session?disableCookieCache=true",
		nil, bearerHeaders(token, ""), &resp)
	return struct {
		Email  string
		UserID string
	}{resp.User.Email, resp.User.ID}, err
}

// listOrganizations returns the user's workspaces (personal first when the
// org id matches the user id).
func listOrganizations(ctx context.Context, token string) ([]Team, error) {
	var orgs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	h := bearerHeaders(token, dashboardURL)
	if err := doJSON(ctx, http.MethodGet, relayURL+"/api/auth/organization/list", nil, h, &orgs); err != nil {
		return nil, err
	}
	teams := make([]Team, len(orgs))
	for i, o := range orgs {
		teams[i] = Team{ID: o.ID, Name: o.Name, Slug: o.Slug}
	}
	return teams, nil
}

// setActiveOrganization marks orgID the session's active workspace. The
// relay's project/API-key procedures resolve the team from the session, so
// this must run before listing/creating under the chosen workspace.
func setActiveOrganization(ctx context.Context, token, orgID string) error {
	return doJSON(ctx, http.MethodPost, relayURL+"/api/auth/organization/set-active",
		map[string]string{"organizationId": orgID}, bearerHeaders(token, dashboardURL), nil)
}

// ListProjects returns the projects under teamID (marks it active first).
func ListProjects(ctx context.Context, token string, team Team) ([]Project, error) {
	if err := setActiveOrganization(ctx, token, team.ID); err != nil {
		return nil, err
	}
	return getProjects(ctx, token, team.ID)
}

// CreateProject makes a project named name under teamID and returns it.
func CreateProject(ctx context.Context, token string, team Team, name string) (Project, error) {
	if err := setActiveOrganization(ctx, token, team.ID); err != nil {
		return Project{}, err
	}
	return createProject(ctx, token, team.ID, name)
}

// SignOut closes the remote session; a 401 (already gone) is not an error.
func SignOut(ctx context.Context, token string) error {
	status, err := doJSONStatus(ctx, http.MethodPost, relayURL+"/api/auth/sign-out",
		map[string]string{}, bearerHeaders(token, dashboardURL), nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusUnauthorized {
		return fmt.Errorf("sign out failed (HTTP %d)", status)
	}
	return nil
}

func bearerHeaders(token, origin string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + token}
	if origin != "" {
		h["Origin"] = origin
	}
	return h
}

// doJSON performs an HTTP call with a JSON body and decodes a JSON reply,
// erroring on non-2xx with the server's message when present.
func doJSON(ctx context.Context, method, url string, body any, headers map[string]string, out any) error {
	status, err := doJSONStatus(ctx, method, url, body, headers, out)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s failed (HTTP %d)", method, url, status)
	}
	return nil
}

// doJSONStatus is doJSON but returns the status so callers can branch on it.
func doJSONStatus(ctx context.Context, method, url string, body any, headers map[string]string, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

const defaultPollIntervalSeconds = 5

// pollSleepOverride, when non-nil, replaces the poll sleep (tests collapse the
// 5s wait). Production code leaves it nil.
var pollSleepOverride func(context.Context, time.Duration) error

func sleepCtx(ctx context.Context, d time.Duration) error {
	if pollSleepOverride != nil {
		return pollSleepOverride(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
