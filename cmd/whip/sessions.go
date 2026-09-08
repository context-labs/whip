package main

import (
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

// `whip sessions` — list stored sessions, newest first. The scriptable
// companion to `whip run`: find a session, then resume it in the TUI with
// `whip --resume <id>` (or `-r <id>`), `whip -c` for the newest in this dir,
// or `whip --browse` for the interactive picker — or inspect from a script.
func sessionsCLI() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := session.Open(dir + "/sessions.db")
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	metas, err := st.Recent(50)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Println("no sessions yet")
		return nil
	}
	for _, mt := range metas {
		title := mt.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Printf("%s  %-40s  %s  %s\n", mt.ID, trunc(title, 40), mt.Model, ago(mt.UpdatedAt))
	}
	return nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}
