package config

import (
	"errors"
	"sync"
	"testing"
)

func TestVersionedConfigurationRejectsConflictsAndPreservesOtherFields(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	c, revision, err := ReadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	c.Theme = "light"
	c.UpsertOpenRouter("secret", false)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateVersioned(revision, func(c *Config) error { c.DefaultEffort = "high"; return nil }); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update: %v", err)
	}
	_, revision, err = ReadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	c, next, err := UpdateVersioned(revision, func(c *Config) error { c.DefaultEffort = "high"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if next == revision || c.Theme != "light" || c.Providers["openrouter"].APIKey != "secret" {
		t.Fatal("patch lost unrelated configuration")
	}
}

func TestVersionedConfigurationSerializesCompetingUpdates(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	_, revision, err := ReadVersioned()
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, effort := range []string{"high", "medium"} {
		wg.Go(func() {
			_, _, err := UpdateVersioned(revision, func(c *Config) error { c.DefaultEffort = effort; return nil })
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}
