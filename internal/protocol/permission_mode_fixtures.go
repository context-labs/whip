package protocol

import "encoding/json"

func permissionModeFixtures() []Fixture {
	result := make([]Fixture, 0, 15)
	for _, entry := range []struct {
		name, raw string
		valid     bool
	}{
		{"SetPermissionDenialParams", `{"edit_id":"deny","session_id":"root","expected_revision":"9007199254740993","deny_interactive":true}`, true},
		{"PermissionDenialEdit", `{"id":"Deny.Mixed-Case","session_id":"root","expected_revision":"9007199254740993","deny_interactive":false,"previous_denial":true,"policy":{"tree_id":"tree","deny_interactive":false,"mode":"automatic","revision":"9007199254740994","updated_at":"2026-09-28T00:00:00Z"},"created_at":"2026-09-28T00:00:00Z"}`, true},
		{"SetPermissionDenialParams", `{"edit_id":"deny","session_id":"root","expected_revision":"1"}`, false},
		{"SetPermissionDenialParams", `{"edit_id":"deny","session_id":"root","expected_revision":"1","deny_interactive":"true"}`, false},
		{"PermissionPolicy", `{"tree_id":"tree","deny_interactive":false,"mode":"automatic","revision":"9007199254740993","updated_at":"2026-09-28T00:00:00Z"}`, true},
		{"SetPermissionModeParams", `{"edit_id":"Edit.Mixed-Case","session_id":"root","expected_revision":"9007199254740993","mode":"automatic"}`, true},
		{"PermissionModeEditParams", `{"session_id":"root","edit_id":"Edit.Mixed-Case"}`, true},
		{"PermissionModeEdit", `{"id":"Edit.Mixed-Case","session_id":"root","expected_revision":"9007199254740993","mode":"automatic","previous_mode":"prompt","policy":{"tree_id":"tree","deny_interactive":false,"mode":"automatic","revision":"9007199254740994","updated_at":"2026-09-28T00:00:00Z"},"created_at":"2026-09-28T00:00:00Z"}`, true},
		{"DefaultPermissionMode", `{"mode":"prompt","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, true},
		{"SetDefaultPermissionModeParams", `{"mode":"automatic","expected_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, true},
		{"SetPermissionModeParams", `{"edit_id":"edit","session_id":"root","expected_revision":"0","mode":"automatic"}`, false},
		{"SetPermissionModeParams", `{"edit_id":"edit","session_id":"root","expected_revision":"1","mode":"FullAccess"}`, false},
		{"SetDefaultPermissionModeParams", `{"mode":"","expected_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, false},
		{"SetDefaultPermissionModeParams", `{"mode":"prompt","expected_revision":"1"}`, false},
	} {
		result = append(result, Fixture{Type: entry.name, Value: json.RawMessage(entry.raw), Valid: entry.valid})
	}
	return result
}
