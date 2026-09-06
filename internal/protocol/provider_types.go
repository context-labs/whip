package protocol

type ProviderLoginList struct {
	Flows []ProviderLoginStatus `json:"flows"`
}
type ProviderNameParams struct {
	Provider string `json:"provider"`
}
type ProviderStatus struct {
	Provider       string   `json:"provider"`
	Configured     bool     `json:"configured"`
	KeySource      string   `json:"key_source"`
	Email          string   `json:"email,omitempty"`
	ProjectID      string   `json:"project_id,omitempty"`
	ProjectName    string   `json:"project_name,omitempty"`
	MachineKeyName string   `json:"machine_key_name,omitempty"`
	Warnings       []string `json:"warnings"`
}
