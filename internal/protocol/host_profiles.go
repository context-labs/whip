package protocol

// HostProfile is a safe saved attachment target, without connection state or secrets.
type HostProfile struct {
	ID              ID     `json:"id"`
	Name            string `json:"name" pattern:"^[^\\x00-\\x1f\\x7f]{1,256}$"`
	URL             string `json:"url" pattern:"^https?://[A-Za-z0-9._:\\[\\]-]+/?$"`
	RuntimeID       ID     `json:"runtime_id"`
	ConnectOnLaunch bool   `json:"connect_on_launch"`
}

type HostProfiles struct {
	Revision string        `json:"revision" pattern:"^[a-f0-9]{64}$"`
	Profiles []HostProfile `json:"profiles"`
}

type SetHostProfilesParams struct {
	ExpectedRevision string        `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Profiles         []HostProfile `json:"profiles"`
}
