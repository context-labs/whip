package config

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	providerKeyFileLimit   = 16 << 10
	providerEnvFileLimit   = 1 << 20
	providerSourceLimit    = 32
	providerNamedFileLimit = 128
)

// ProviderKeySources identifies host-owned files containing named provider keys.
// Values remain in those files; providers persist only APIKeyEnv references.
type ProviderKeySources struct {
	EnvFiles       []string          `json:"envFiles,omitempty"`
	KeyFiles       map[string]string `json:"keyFiles,omitempty"`
	KeyDirectories []string          `json:"keyDirectories,omitempty"`
}

// IsZero reports whether only inherited environment/account sources are enabled.
func (s ProviderKeySources) IsZero() bool {
	return len(s.EnvFiles) == 0 && len(s.KeyFiles) == 0 && len(s.KeyDirectories) == 0
}

func (s ProviderKeySources) clone() ProviderKeySources {
	s.EnvFiles = slices.Clone(s.EnvFiles)
	s.KeyFiles = maps.Clone(s.KeyFiles)
	s.KeyDirectories = slices.Clone(s.KeyDirectories)
	return s
}

type namedProviderKey struct {
	value            string
	status           KeyStatus
	err              error
	openAICompatible bool
}

// CredentialSnapshot holds short-lived credentials and provenance for one host
// operation. Never persist it or return it through a protocol response.
type CredentialSnapshot struct {
	named       map[string]namedProviderKey
	environment map[string]string
	machineKey  string
	infKey      string
	errors      []error
}

// DiscoverCredentials reads configured sources once without changing the process
// environment or executing commands. Pass the already-loaded config; this method
// never calls Load, including when used inside a configuration update callback.
// With no config it preserves environment and account-only resolution.
func DiscoverCredentials(configs ...*Config) *CredentialSnapshot {
	snapshot := &CredentialSnapshot{named: map[string]namedProviderKey{}, environment: map[string]string{}}
	names := map[string]bool{"OPENAI_BASE_URL": true, "OPENAI_API_BASE": true}
	for _, preset := range ProviderPresets() {
		for _, name := range preset.EnvironmentVariables {
			names[name] = true
		}
	}
	var sources ProviderKeySources
	if len(configs) > 0 && configs[0] != nil {
		sources = configs[0].ProviderKeySources
	}
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, provider := range cfg.Providers {
			if isEnvName(provider.APIKeyEnv) {
				names[provider.APIKeyEnv] = true
			}
			if name := providerReferenceName(provider.APIKey); name != "" {
				names[name] = true
			}
		}
	}
	for name := range sources.KeyFiles {
		if isEnvName(name) {
			names[name] = true
		}
	}
	// Capture only named variables; unrelated host secrets are never retained.
	for name := range names {
		snapshot.environment[name] = os.Getenv(name)
	}
	snapshot.machineKey, snapshot.infKey = whipInferenceNetKey(), infKey()
	if len(sources.EnvFiles) > providerSourceLimit || len(sources.KeyDirectories) > providerSourceLimit || len(sources.KeyFiles) > providerNamedFileLimit {
		err := errors.New("providerKeySources exceeds the supported source count")
		snapshot.errors = append(snapshot.errors, err)
		for name := range names {
			if value := snapshot.environment[name]; strings.TrimSpace(value) != "" {
				snapshot.named[name] = namedProviderKey{value: value, status: KeyStatus{Source: "environment", Environment: name}, openAICompatible: true}
			} else {
				snapshot.named[name] = namedProviderKey{status: KeyStatus{Source: "key_file", Environment: name}, err: err}
			}
		}
		return snapshot
	}
	// Parse each env file once. Invalid configured files fail closed for names
	// unresolved by higher-priority sources; diagnostics never contain values.
	type envSource struct {
		path   string
		values map[string]string
		err    error
	}
	envFiles := make([]envSource, 0, len(sources.EnvFiles))
	for _, filename := range sources.EnvFiles {
		path, err := providerSourcePath(filename)
		var values map[string]string
		if err == nil {
			var data []byte
			data, err = readProviderFile(path, providerEnvFileLimit)
			if err == nil {
				values, err = parseProviderEnvFile(data, path)
			}
		}
		if err != nil {
			snapshot.errors = append(snapshot.errors, err)
		}
		envFiles = append(envFiles, envSource{path: filename, values: values, err: err})
	}
	mapped := map[string]namedProviderKey{}
	for name, filename := range sources.KeyFiles {
		status := KeyStatus{Source: "key_file", Environment: name, Path: filename}
		if !isEnvName(name) {
			snapshot.errors = append(snapshot.errors, errors.New("providerKeySources.keyFiles contains an invalid variable name"))
			continue
		}
		key := readNamedProviderFile(filename, status)
		mapped[name] = key
		if key.err != nil {
			snapshot.errors = append(snapshot.errors, key.err)
		}
	}
	type keyDirectory struct {
		path   string
		root   *os.Root
		err    error
		values map[string]namedProviderKey
	}
	directories := make([]keyDirectory, 0, len(sources.KeyDirectories))
	for _, filename := range sources.KeyDirectories {
		directory := keyDirectory{path: filename, values: map[string]namedProviderKey{}}
		path, err := providerSourcePath(filename)
		if err == nil {
			directory.root, err = os.OpenRoot(path)
		}
		directory.err = err
		if err != nil {
			snapshot.errors = append(snapshot.errors, fmt.Errorf("provider key directory %q is unavailable", filename))
		}
		if directory.root != nil {
			defer func() { _ = directory.root.Close() }()
		}
		directories = append(directories, directory)
	}
	for name := range names {
		if value := snapshot.environment[name]; strings.TrimSpace(value) != "" {
			snapshot.named[name] = namedProviderKey{value: value, status: KeyStatus{Source: "environment", Environment: name}, openAICompatible: true}
			continue
		}
		if key, exists := mapped[name]; exists {
			key.openAICompatible = sourceOpenAICompatible(func(n string) (string, error) { k := mapped[n]; return k.value, k.err })
			snapshot.named[name] = key
			continue
		}
		found := false
		for _, source := range envFiles {
			value, exists := source.values[name]
			if source.err == nil && !exists {
				continue
			}
			status := KeyStatus{Source: "env_file", Environment: name, Path: source.path}
			snapshot.named[name] = namedProviderKey{value: value, status: status, err: source.err, openAICompatible: sourceOpenAICompatible(func(n string) (string, error) { return source.values[n], nil })}
			found = true
			break
		}
		if found {
			continue
		}
		for i := range directories {
			directory := &directories[i]
			status := KeyStatus{Source: "key_file", Environment: name, Path: filepath.Join(directory.path, name)}
			if directory.err != nil {
				snapshot.named[name] = namedProviderKey{status: status, err: fmt.Errorf("provider key directory %q is unavailable", directory.path)}
				break
			}
			read := func(n string) (namedProviderKey, bool) {
				if key, exists := directory.values[n]; exists {
					return key, true
				}
				info, err := directory.root.Stat(n)
				if errors.Is(err, os.ErrNotExist) {
					return namedProviderKey{}, false
				}
				if err != nil || !info.Mode().IsRegular() {
					key := namedProviderKey{status: KeyStatus{Source: "key_file", Environment: n, Path: filepath.Join(directory.path, n)}, err: fmt.Errorf("provider key file %q must be a readable regular file", filepath.Join(directory.path, n))}
					directory.values[n] = key
					return key, true
				}
				file, err := directory.root.Open(n)
				if errors.Is(err, os.ErrNotExist) {
					return namedProviderKey{}, false
				}
				key := namedProviderKey{status: KeyStatus{Source: "key_file", Environment: n, Path: filepath.Join(directory.path, n)}}
				if err != nil {
					key.err = fmt.Errorf("provider key file %q is unavailable", key.status.Path)
				} else {
					data, readErr := readProviderHandle(file, providerKeyFileLimit, key.status.Path)
					_ = file.Close()
					key.value, key.err = rawProviderKey(data, key.status.Path, readErr)
				}
				directory.values[n] = key
				return key, true
			}
			key, exists := read(name)
			if !exists {
				continue
			}
			key.openAICompatible = sourceOpenAICompatible(func(n string) (string, error) { k, _ := read(n); return k.value, k.err })
			snapshot.named[name] = key
			if key.err != nil {
				snapshot.errors = append(snapshot.errors, key.err)
			}
			break
		}
	}
	return snapshot
}

// Err reports source configuration/read errors without exposing credential values.
func (s *CredentialSnapshot) Err() error {
	if s == nil {
		return nil
	}
	return errors.Join(s.errors...)
}

func providerReferenceName(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && isEnvName(value[2:len(value)-1]) {
		return value[2 : len(value)-1]
	}
	if strings.HasPrefix(value, "$") && isEnvName(value[1:]) {
		return value[1:]
	}
	return ""
}

func (s *CredentialSnapshot) providerNamedKey(provider Provider, name string) (string, KeyStatus, error) {
	key, exists := s.named[name]
	if !exists {
		key = namedProviderKey{value: s.environment[name], status: KeyStatus{Source: "environment", Environment: name}, openAICompatible: true}
		// A stand-alone Provider can name a variable absent from the preset/config.
		if _, captured := s.environment[name]; !captured {
			key.value = os.Getenv(name)
		}
	}
	if key.err != nil {
		return "", key.status, key.err
	}
	if name == "OPENAI_API_KEY" && strings.TrimRight(provider.BaseURL, "/") == "https://api.openai.com/v1" {
		matches := sourceOpenAICompatible(func(n string) (string, error) { return s.environment[n], nil })
		if !matches || !key.openAICompatible {
			key.status.Error = "OpenAI key source specifies a different endpoint"
			return "", key.status, nil
		}
	}
	if strings.TrimSpace(key.value) == "" {
		return "", key.status, nil
	}
	return key.value, key.status, nil
}

func sourceOpenAICompatible(lookup func(string) (string, error)) bool {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"} {
		value, err := lookup(name)
		if err != nil {
			return false
		}
		value = strings.TrimSpace(value)
		if value != "" && strings.TrimRight(value, "/") != "https://api.openai.com/v1" {
			return false
		}
	}
	return true
}

func providerSourcePath(filename string) (string, error) {
	if strings.HasPrefix(filename, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("could not resolve provider key source home")
		}
		filename = filepath.Join(home, filename[2:])
	}
	if !filepath.IsAbs(filename) {
		return "", fmt.Errorf("provider key source %q must be absolute or start with ~/", filename)
	}
	return filename, nil
}

func readNamedProviderFile(filename string, status KeyStatus) namedProviderKey {
	key := namedProviderKey{status: status}
	path, err := providerSourcePath(filename)
	var data []byte
	if err == nil {
		data, err = readProviderFile(path, providerKeyFileLimit)
	}
	key.value, key.err = rawProviderKey(data, filename, err)
	return key
}

func rawProviderKey(data []byte, filename string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("provider key file %q contains an invalid key", filename)
	}
	return value, nil
}

func readProviderFile(filename string, limit int64) ([]byte, error) {
	// Stat first avoids blocking while opening a named pipe. Check the opened
	// handle too so replacement with a non-regular file cannot be consumed.
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("provider key source %q must be a readable regular file", filename)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("provider key source %q is unavailable", filename)
	}
	defer file.Close()
	return readProviderHandle(file, limit, filename)
}

func readProviderHandle(file *os.File, limit int64, filename string) ([]byte, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("provider key source %q must be a readable regular file", filename)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("provider key source %q exceeds its size limit", filename)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("provider key source %q could not be read within its size limit", filename)
	}
	return data, nil
}

func parseProviderEnvFile(data []byte, filename string) (map[string]string, error) {
	values := map[string]string{}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("provider environment file %q has invalid text encoding", filename)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), providerEnvFileLimit+1)
	line := 0
	invalid := func() (map[string]string, error) {
		return nil, fmt.Errorf("provider environment file %q line %d has unsupported syntax", filename, line)
	}
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "export\t") {
			text = strings.TrimSpace(text[6:])
		}
		name, value, ok := strings.Cut(text, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || !isEnvName(name) {
			return invalid()
		}
		if value != "" && (value[0] == '\'' || value[0] == '"') {
			quote := value[0]
			end := strings.IndexByte(value[1:], quote)
			if end < 0 {
				return invalid()
			}
			tail := strings.TrimSpace(value[end+2:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return invalid()
			}
			value = value[1 : end+1]
			// Backslash escapes/multiline values are outside this deliberate subset.
			if strings.Contains(value, "\\") {
				return invalid()
			}
		} else {
			if index := strings.IndexByte(value, '#'); index >= 0 && (index == 0 || value[index-1] == ' ' || value[index-1] == '\t') {
				value = strings.TrimSpace(value[:index])
			}
			if strings.ContainsAny(value, " \t'\";\\") {
				return invalid()
			}
		}
		if strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.Contains(value, "$(") || strings.Contains(value, "`") {
			return invalid()
		}
		if len(value) > providerKeyFileLimit {
			return invalid()
		}
		// The first assignment wins, including an explicit empty value.
		if _, exists := values[name]; !exists {
			values[name] = value
		}
	}
	if scanner.Err() != nil {
		return invalid()
	}
	return values, nil
}

// PersistDiscoveredProviders atomically adds only missing supported named-key
// routes. Existing config, defaults and disabled IDs win. Source I/O happens
// outside the configuration lock and revision conflicts retry from fresh state.
func PersistDiscoveredProviders(ctx context.Context) (*Config, string, error) {
	for range 8 {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		cfg, revision, err := ReadVersioned()
		if err != nil {
			return nil, "", err
		}
		credentials := DiscoverCredentials(cfg)
		candidates := map[string]Provider{}
		for _, preset := range ProviderPresets() {
			if _, exists := cfg.Providers[preset.ID]; exists || slices.Contains(cfg.DisabledProviders, preset.ID) {
				continue
			}
			status := credentials.KeyStatus(preset.Provider)
			if !status.Available || status.Environment == "" {
				continue
			}
			route := preset.Provider
			route.APIKeyEnv = status.Environment
			candidates[preset.ID] = route
		}
		updated, nextRevision, updateErr := UpdateVersionedIfChanged(revision, func(current *Config) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(candidates) == 0 {
				return nil
			}
			if current.Providers == nil {
				current.Providers = map[string]Provider{}
			}
			for id, candidate := range candidates {
				if _, exists := current.Providers[id]; !exists && !slices.Contains(current.DisabledProviders, id) {
					current.Providers[id] = candidate
				}
			}
			return nil
		})
		if errors.Is(updateErr, ErrRevisionConflict) {
			continue
		}
		if updateErr != nil {
			return cfg, revision, errors.Join(updateErr, credentials.Err())
		}
		return updated, nextRevision, credentials.Err()
	}
	return nil, "", ErrRevisionConflict
}

// AvailableEnvironmentVariable returns the first available preset key name,
// including configured file sources and the canonical OpenAI endpoint guard.
func (s *CredentialSnapshot) AvailableEnvironmentVariable(preset ProviderPreset) string {
	if s == nil {
		s = DiscoverCredentials()
	}
	for _, name := range preset.EnvironmentVariables {
		key, _, err := s.providerNamedKey(preset.Provider, name)
		if err != nil {
			return ""
		}
		if key != "" {
			return name
		}
	}
	return ""
}

func providerCredentialConfigs(provider Provider, configs []*Config) []*Config {
	if len(configs) == 0 {
		configs = []*Config{nil}
	}
	result := slices.Clone(configs)
	return append(result, &Config{Providers: map[string]Provider{"candidate": provider}})
}
