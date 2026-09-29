package config

import "github.com/context-labs/whip/internal/secretref"

const SecretCmdTimeout = secretref.SecretCmdTimeout

var (
	isEnvName            = secretref.IsEnvName
	isEnvRefBody         = secretref.IsEnvRefBody
	ResolveSecret        = secretref.ResolveSecret
	ResolveSecretContext = secretref.ResolveSecretContext
	IsWholeRef           = secretref.IsWholeRef
	ExpandTemplate       = secretref.ExpandTemplate
	ResolveEnvMap        = secretref.ResolveEnvMap
	ResolveEnvMapContext = secretref.ResolveEnvMapContext
	ResolveHeader        = secretref.ResolveHeader
	ResolveHeaderContext = secretref.ResolveHeaderContext
)
