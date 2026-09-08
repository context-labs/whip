// Package buildinfo identifies the distribution compiled into an executable.
package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
)

// Name is set to whipcode by that distribution's release build. Renaming the
// executable must not change its configuration or update channel.
var Name = "whip"

// Env names a distribution-owned environment variable.
func Env(suffix string) string { return strings.ToUpper(Name) + "_" + suffix }

// Home resolves application-owned files beneath a supplied user home. Keeping
// resolution separate from directory creation also serves discovery paths.
func Home(userHome string) string {
	if dir := os.Getenv(Env("HOME")); dir != "" {
		return dir
	}
	return filepath.Join(userHome, "."+Name)
}

// Version removes the channel prefix for human-readable version output.
func Version(tag string) string { return strings.TrimPrefix(tag, "whipcode-") }

// Text adapts existing executable instructions without renaming wire identifiers
// or the internal whip-computer helper.
func Text(text string) string {
	if Name == "whip" {
		return text
	}
	return strings.NewReplacer(
		"`npm ci && task build`", "`npm ci && task build:"+Name+"`",
		"github.com/context-labs/whip", "github.com/context-labs/whip",
		"~/.whip", "~/."+Name,
		"WHIP_HOME", Env("HOME"),
		"WHIP_NETWORK", Env("NETWORK"),
		"WHIP_LISTEN", Env("LISTEN"),
		"WHIP_ALLOWED_ORIGINS", Env("ALLOWED_ORIGINS"),
		"WHIP_ALLOWED_HOSTS", Env("ALLOWED_HOSTS"),
		"`whip`", "`"+Name+"`",
		"whip ", Name+" ",
		"whip:", Name+":",
	).Replace(text)
}
