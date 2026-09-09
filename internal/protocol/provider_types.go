package protocol

type ProviderLoginList struct {
	Flows []ProviderLoginStatus `json:"flows"`
}
type ProviderNameParams struct {
	Provider string `json:"provider"`
}
type ProviderDisconnectParams struct {
	Provider string `json:"provider"`
	Revision string `json:"revision"`
}
type ProviderCatalogParams struct {
	Refresh bool `json:"refresh,omitempty"`
}
type ProviderList struct {
	Revision        string          `json:"revision"`
	DefaultProvider string          `json:"default_provider,omitempty"`
	Providers       []ProviderEntry `json:"providers"`
}
type ProviderEntry struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Custom  bool           `json:"custom"`
	Methods []string       `json:"methods"`
	Status  ProviderStatus `json:"status"`
}
type ProviderLoginBeginParams struct {
	Provider string `json:"provider,omitempty"`
}
type ProviderStatus struct {
	Provider            string   `json:"provider"`
	Configured          bool     `json:"configured"`
	KeySource           string   `json:"key_source"`
	Available           *bool    `json:"available,omitempty"`
	Disabled            bool     `json:"disabled,omitempty"`
	EnvironmentVariable string   `json:"environment_variable,omitempty"`
	AuthMethod          string   `json:"auth_method,omitempty"`
	AuthState           string   `json:"auth_state,omitempty"`
	AccountID           string   `json:"account_id,omitempty"`
	Plan                string   `json:"plan,omitempty"`
	Email               string   `json:"email,omitempty"`
	ProjectID           string   `json:"project_id,omitempty"`
	ProjectName         string   `json:"project_name,omitempty"`
	MachineKeyName      string   `json:"machine_key_name,omitempty"`
	Warnings            []string `json:"warnings"`
}
