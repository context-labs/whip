package lspconfig

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Config declares a trusted host-owned stdio server. Publication grants no
// session authority and never installs a missing executable.
type Config struct {
	Command     []string          `json:"command,omitempty"`
	Extensions  []string          `json:"extensions,omitempty"`
	RootMarkers []string          `json:"rootMarkers,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
}

func ValidateConfig(values map[string]Config) error {
	if len(values) > 16 {
		return errors.New("at most 16 language server declarations are allowed")
	}
	for name, value := range values {
		if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\r\n/\\") {
			return errors.New("invalid language server name")
		}
		if len(value.Command) > 32 || len(value.Extensions) > 32 || len(value.RootMarkers) > 32 || len(value.Env) > 32 {
			return fmt.Errorf("language server %s exceeds a declaration count limit", name)
		}
		for _, arg := range value.Command {
			if arg == "" || len(arg) > 4096 || !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("language server %s has an invalid command argument", name)
			}
		}
		for _, extension := range value.Extensions {
			if len(extension) < 2 || len(extension) > 32 || extension[0] != '.' || strings.ContainsAny(extension, "\x00\r\n/\\") || !utf8.ValidString(extension) {
				return fmt.Errorf("language server %s has an invalid extension", name)
			}
		}
		for _, marker := range value.RootMarkers {
			if !filepath.IsLocal(marker) || filepath.Base(marker) != marker || marker == "." || len(marker) > 255 || !utf8.ValidString(marker) || strings.ContainsAny(marker, "\x00\r\n\\") {
				return fmt.Errorf("language server %s has an invalid root marker", name)
			}
		}
		for key, value := range value.Env {
			if key == "" || len(key) > 128 || len(value) > 4096 || !utf8.ValidString(key+value) || strings.ContainsAny(key, "=\x00\r\n") || strings.ContainsRune(value, 0) {
				return fmt.Errorf("language server %s has an invalid environment entry", name)
			}
		}
		if value.Enabled == nil || *value.Enabled {
			if _, builtin := builtinServers[name]; !builtin && (len(value.Command) == 0 || len(value.Extensions) == 0) {
				return fmt.Errorf("language server %s requires command and extensions", name)
			}
		}
	}
	if len(FromConfigMap(values)) > 16 {
		return errors.New("at most 16 enabled language server declarations are allowed")
	}
	return nil
}

// CloneSpec copies all mutable declaration collections.
func CloneSpec(spec ServerSpec) ServerSpec {
	spec.Command = slices.Clone(spec.Command)
	spec.Extensions = slices.Clone(spec.Extensions)
	spec.RootMarkers = slices.Clone(spec.RootMarkers)
	spec.Env = maps.Clone(spec.Env)
	return spec
}

// ServerSpec describes how to match and spawn one language server.
type ServerSpec struct {
	Command     []string          // argv; nil for a disabled entry
	Extensions  []string          // file extensions served, e.g. [".go"]
	RootMarkers []string          // files that mark a project root
	Env         map[string]string // extra env layered over whip's
	Disabled    bool
}

// builtinServers is the shipped registry. Adding a built-in is one row.
var builtinServers = map[string]ServerSpec{
	"gopls": {
		Command:     []string{"gopls"},
		Extensions:  []string{".go"},
		RootMarkers: []string{"go.work", "go.mod", "go.sum"},
	},
}

// FromConfigMap converts the config-file "lsp" block into specs merged over
// the built-ins: a user entry with disabled=true removes the built-in, an
// entry with a command replaces/extends it (extensions/rootMarkers default to
// the built-in's when omitted).
func FromConfigMap(in map[string]Config) map[string]ServerSpec {
	out := make(map[string]ServerSpec, len(builtinServers)+len(in))
	for name, spec := range builtinServers {
		out[name] = CloneSpec(spec)
	}
	for name, c := range in {
		existing := out[name]
		if c.Enabled != nil && !*c.Enabled {
			delete(out, name)
			continue
		}
		spec := existing
		if len(c.Command) > 0 {
			spec.Command = c.Command
		}
		if len(c.Extensions) > 0 {
			spec.Extensions = c.Extensions
		}
		if len(c.RootMarkers) > 0 {
			spec.RootMarkers = c.RootMarkers
		}
		if len(c.Env) > 0 {
			spec.Env = c.Env
		}
		out[name] = CloneSpec(spec)
	}
	return out
}
