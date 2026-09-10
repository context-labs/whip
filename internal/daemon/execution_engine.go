package daemon

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
)

func executionEngines() []protocol.ExecutionEngine {
	descriptors := rlm.Engines()
	result := make([]protocol.ExecutionEngine, 0, len(descriptors))
	for _, descriptor := range descriptors {
		result = append(result, protocol.ExecutionEngine{ID: descriptor.ID, Language: descriptor.Language, Label: descriptor.Label, Build: descriptor.Build, ABI: descriptor.ABI, Profile: descriptor.Profile, Fidelity: descriptor.Fidelity, GuideSHA256: descriptor.GuideSHA256, BridgeSHA256: descriptor.BridgeSHA256, Features: descriptor.Features, DefaultLimits: descriptor.Limits})
	}
	return result
}

func configuredExecutionEngine() string {
	cfg, _, err := config.ReadVersioned()
	if err != nil {
		return "starlark"
	}
	return cfg.RLM.Engine()
}

type checkpointStore struct{ node *AgentSession }

func (store checkpointStore) Load(ctx context.Context) (*rlm.Checkpoint, error) {
	node := store.node
	if node.root == nil || node.id == "" {
		return nil, nil
	}
	return routeControlValue(node.root, ctx, func(actorCtx context.Context) (*rlm.Checkpoint, error) {
		envelope, image, err := node.root.store.LoadAgentCheckpoint(actorCtx, node.root.ID(), node.id)
		if err != nil || len(envelope) == 0 {
			return nil, err
		}
		checkpoint := &rlm.Checkpoint{Data: image}
		if err := json.Unmarshal(envelope, &checkpoint.Envelope); err != nil {
			return nil, err
		}
		return checkpoint, nil
	})
}

func (store checkpointStore) Save(ctx context.Context, checkpoint rlm.Checkpoint) error {
	node := store.node
	if node.root == nil || node.id == "" {
		return nil
	}
	checkpoint.Envelope.RootID, checkpoint.Envelope.AgentID = node.root.ID(), node.id
	descriptor, err := rlm.ResolveEngine(node.runtime.engine)
	if err != nil {
		return err
	}
	if err := checkpoint.Validate(descriptor); err != nil {
		return err
	}
	encoded, err := json.Marshal(checkpoint.Envelope)
	if err != nil {
		return err
	}
	return node.root.routeControl(ctx, func(actorCtx context.Context) error {
		return node.root.store.SaveAgentCheckpoint(actorCtx, node.root.ID(), node.id, encoded, checkpoint.Data)
	})
}
