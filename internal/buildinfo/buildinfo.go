// Package buildinfo defines the canonical application identity and update owner.
package buildinfo

import (
	"os"
	"path/filepath"
)

// Name is the sole supported CLI name, independent of the executable filename.
const Name = "whipcode"

// UpdateOwner is set to desktop for the payload installed by the desktop app.
// Its GUI and canonical executable must be updated through the same release.
var UpdateOwner = "standalone"

// Env names an application-owned environment variable.
func Env(suffix string) string { return "WHIPCODE_" + suffix }

// Home resolves application-owned files without creating the directory.
func Home(userHome string) string {
	if dir := os.Getenv(Env("HOME")); dir != "" {
		return dir
	}
	return filepath.Join(userHome, ".whipcode")
}
