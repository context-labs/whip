package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

// `whipcode sessions` — list stored sessions, newest first. The scriptable
// companion to `whipcode run`: find a session, then resume it in the TUI or
// inspect it from a script. `whipcode sessions export <root>` renders a session's
// trace as OTLP/JSON.
func sessionsCLI() error {
	if args := flag.Args(); len(args) > 1 && args[1] == "export" {
		return sessionsExportCLI(args[2:])
	}
	if len(flag.Args()) > 1 {
		return errors.New("usage: whipcode sessions [export <root>]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	var result protocol.RecentTreesResult
	if err := connection.Call(ctx, "trees.recent", protocol.RecentTreesParams{Limit: 50}, &result); err != nil {
		return err
	}
	if len(result.Items) == 0 {
		fmt.Println("no sessions yet")
		return nil
	}
	for _, item := range result.Items {
		title := "(untitled)"
		if item.Tree.Metadata.Title != nil && *item.Tree.Metadata.Title != "" {
			title = *item.Tree.Metadata.Title
		}
		at, err := time.Parse(time.RFC3339Nano, item.LastActivityAt)
		if err != nil {
			return fmt.Errorf("decode recent session activity: %w", err)
		}
		fmt.Printf("%s  %-40s  %s  %s\n", item.RootID, trunc(title, 40), item.Model.Name, ago(at))
	}
	return nil
}

func trunc(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
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
