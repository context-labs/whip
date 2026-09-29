package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const nativePreferencesFile = "client-preferences.json"

// These are client presentation choices only. The directory is explicitly
// supplied by the native entry point; remote session paths never select it.
// Keep the other terminal presentation fields when changing a single setting.
type nativePreferences struct {
	Theme         string `json:"theme"`
	Sidebar       *bool  `json:"sidebar"`
	Repl          *bool  `json:"repl"`
	Panel         string `json:"panel"`
	Mouse         *bool  `json:"mouse"`
	Thinking      *bool  `json:"thinking"`
	CollapsePaste *bool  `json:"collapse_paste"`
}

func readNativePreferences(directory string) (nativePreferences, error) {
	var value nativePreferences
	root, err := nativePreferencesRoot(directory)
	if err != nil {
		return value, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(nativePreferencesFile)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return value, errors.New("client preferences must be a regular file of at most 16 KiB")
	}
	file, err := root.Open(nativePreferencesFile)
	if err != nil {
		return value, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil {
		return value, err
	}
	if len(data) > 16<<10 {
		return value, errors.New("client preferences exceed 16 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("read client preferences: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, errors.New("client preferences contain trailing data")
	}
	return value, value.validate()
}

func (p nativePreferences) validate() error {
	if len(p.Theme) > 128 || len(p.Panel) > 64 {
		return errors.New("client preference name is too long")
	}
	for _, text := range []string{p.Theme, p.Panel} {
		if strings.ContainsAny(text, "\r\n\x00\x1b") {
			return errors.New("client preference name contains control characters")
		}
	}
	return nil
}

func saveNativePreferences(directory string, value nativePreferences) error {
	if err := value.validate(); err != nil {
		return err
	}
	root, err := nativePreferencesRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if info, err := root.Lstat(nativePreferencesFile); err == nil && !info.Mode().IsRegular() {
		return errors.New("client preferences must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	name := ".client-preferences-" + uuid.NewString()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	_, writeErr := file.Write(append(data, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return root.Rename(name, nativePreferencesFile)
}

func nativePreferencesRoot(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) {
		return nil, errors.New("an explicit absolute client preferences directory is required")
	}
	return os.OpenRoot(directory)
}
