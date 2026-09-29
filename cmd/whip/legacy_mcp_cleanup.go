package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/legacy/session"
)

func deleteDaemonSession(clientID, rootID string) error {
	if rootID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cleanupID := daemonCommandID(clientID, "cleanup")
	connection, err := connectDaemon(ctx, "automation", cleanupID, nil)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	payload, err := json.Marshal(map[string]string{"root_id": rootID})
	if err != nil {
		return err
	}
	result, err := connection.Command(ctx, daemon.CommandParams{
		CommandID: cleanupID + "-delete-" + rootID,
		Scope:     string(session.CommandScopeDaemon),
		Operation: "session.delete",
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	if result.Status != "succeeded" {
		return errors.New(result.Error)
	}
	return nil
}
