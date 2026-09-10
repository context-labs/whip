package protocol

type ProviderLoginList struct {
	Flows []ProviderLoginStatus `json:"flows"`
}
type ProviderNameParams struct {
	Provider string `json:"provider"`
}

// ProviderDefinition is the editable, non-secret portion of a host connection.
type ProviderDefinition struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	API     string `json:"api"`
}

// ProviderCredential is ephemeral and must never enter command history.
type ProviderCredential struct {
	Mode                string `json:"mode"`
	Key                 string `json:"key,omitempty"`
	EnvironmentVariable string `json:"environment_variable,omitempty"`
}

type ProviderManualModel struct {
	Alias     string `json:"alias"`
	ID        string `json:"id"`
	Context   int    `json:"context,omitempty"`
	MaxOutput int    `json:"max_output,omitempty"`
}

type ProviderCreateParams struct {
	Revision        string               `json:"revision"`
	Provider        string               `json:"provider"`
	Definition      ProviderDefinition   `json:"definition"`
	Credential      ProviderCredential   `json:"credential"`
	ManualModel     *ProviderManualModel `json:"manual_model,omitempty"`
	AllowUnverified bool                 `json:"allow_unverified,omitempty"`
}

type ProviderUpdateParams struct {
	Revision        string               `json:"revision"`
	Provider        string               `json:"provider"`
	Name            *string              `json:"name,omitempty"`
	BaseURL         *string              `json:"base_url,omitempty"`
	Credential      *ProviderCredential  `json:"credential,omitempty"`
	ManualModel     *ProviderManualModel `json:"manual_model,omitempty"`
	AllowUnverified bool                 `json:"allow_unverified,omitempty"`
}

type ProviderCredentialSummary struct {
	Mode                string `json:"mode"`
	Configured          bool   `json:"configured"`
	EnvironmentVariable string `json:"environment_variable,omitempty"`
	CredentialPath      string `json:"credential_path,omitempty"`
	Available           *bool  `json:"available,omitempty"`
}

type ProviderConfiguredModel struct {
	Alias     string `json:"alias"`
	ID        string `json:"id"`
	Context   int    `json:"context,omitempty"`
	MaxOutput int    `json:"max_output,omitempty"`
}

type ProviderDiscovery struct {
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	ModelCount int    `json:"model_count,omitempty"`
}

type ProviderConfiguration struct {
	Revision        string                    `json:"revision"`
	Provider        string                    `json:"provider"`
	Definition      ProviderDefinition        `json:"definition"`
	Custom          bool                      `json:"custom"`
	Credential      ProviderCredentialSummary `json:"credential"`
	Models          []ProviderConfiguredModel `json:"models"`
	RemovalBlockers []string                  `json:"removal_blockers"`
	Discovery       *ProviderDiscovery        `json:"discovery,omitempty"`
}

type ProviderRemoveParams struct {
	Revision string `json:"revision"`
	Provider string `json:"provider"`
}

type ProviderRemoveResult struct {
	Revision string `json:"revision"`
	Warning  string `json:"warning,omitempty"`
}
type ProviderDisconnectParams struct {
	Provider string `json:"provider"`
	Revision string `json:"revision"`
}
type ProviderCatalogParams struct {
	Refresh  bool   `json:"refresh,omitempty"`
	Provider string `json:"provider,omitempty"`
}
type ProviderListParams struct {
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// ProviderSelection describes local route readiness, not a verified inference call.
type ProviderSelection struct {
	Ready    bool   `json:"ready"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}
type ProviderList struct {
	Revision        string             `json:"revision"`
	DiscoveryError  string             `json:"discovery_error,omitempty"`
	DefaultProvider string             `json:"default_provider,omitempty"`
	Providers       []ProviderEntry    `json:"providers"`
	Selection       *ProviderSelection `json:"selection,omitempty"`
}
type ProviderEntry struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Custom         bool           `json:"custom"`
	Methods        []string       `json:"methods"`
	Status         ProviderStatus `json:"status"`
	Recommended    bool           `json:"recommended,omitempty"`
	SuggestedModel string         `json:"suggested_model,omitempty"`
	Category       string         `json:"category,omitempty"`
	Family         string         `json:"family,omitempty"`
	KeyURL         string         `json:"key_url,omitempty"`
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
	CredentialPath      string   `json:"credential_path,omitempty"`
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
