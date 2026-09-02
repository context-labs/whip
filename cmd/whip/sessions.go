package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

// `whip sessions` — list stored sessions, newest first. The scriptable
// companion to `whip run`: find a session, then resume it in the TUI or
// inspect it from a script.
func sessionsCLI() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientID := daemonClientID("sessions")
	connection, err := connectDaemon(ctx, "automation", clientID, nil)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	payload, err := json.Marshal(map[string]int{"limit": 50})
	if err != nil {
		return err
	}
	result, err := connection.Command(ctx, daemon.CommandParams{
		CommandID: daemonCommandID(clientID, "list"), Scope: string(session.CommandScopeDaemon),
		Operation: "session.list", Payload: payload,
	})
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return errors.New(result.Error)
	}
	var metas []session.Meta
	if err := json.Unmarshal([]byte(result.Output), &metas); err != nil {
		return fmt.Errorf("decode daemon session list: %w", err)
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
