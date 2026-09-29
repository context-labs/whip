package tui

import (
	"errors"
	"os"
	"syscall"
)

// updateNativePreferences serializes native terminal writers while preserving
// unrelated fields. Contention fails visibly instead of blocking the UI or
// silently retrying a choice against a newer value.
func updateNativePreferences(directory string, change func(*nativePreferences)) (nativePreferences, error) {
	root, err := nativePreferencesRoot(directory)
	if err != nil {
		return nativePreferences{}, err
	}
	defer func() { _ = root.Close() }()
	lock, err := root.OpenFile(".client-preferences.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nativePreferences{}, err
	}
	defer func() { _ = lock.Close() }()
	info, err := lock.Stat()
	if err != nil {
		return nativePreferences{}, err
	}
	if !info.Mode().IsRegular() {
		return nativePreferences{}, errors.New("client preferences lock must be a regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nativePreferences{}, errors.New("another terminal is saving preferences; inspect and choose again")
		}
		return nativePreferences{}, err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	value, err := readNativePreferences(directory)
	if err != nil {
		return nativePreferences{}, err
	}
	change(&value)
	if err := saveNativePreferences(directory, value); err != nil {
		return nativePreferences{}, err
	}
	return value, nil
}
