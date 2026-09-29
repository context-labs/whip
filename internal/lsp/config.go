package lsp

import "github.com/context-labs/whip/internal/lspconfig"

// Declarations are pure host values; process ownership stays in this package.
type (
	Config     = lspconfig.Config
	ServerSpec = lspconfig.ServerSpec
)

func ValidateConfig(values map[string]Config) error { return lspconfig.ValidateConfig(values) }
func FromConfigMap(values map[string]Config) map[string]ServerSpec {
	return lspconfig.FromConfigMap(values)
}
