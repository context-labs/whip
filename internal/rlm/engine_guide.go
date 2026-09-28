package rlm

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/context-labs/whip/internal/engine/process"
)

var engineGuideDigests = sync.OnceValue(func() map[string]string {
	result := make(map[string]string)
	for _, descriptor := range process.ExecutionEngines() {
		guide, _ := RuntimeGuide(descriptor.ID, ModuleNames(), nil, nil, "", nil)
		digest := sha256.Sum256([]byte(guide))
		result[descriptor.ID] = hex.EncodeToString(digest[:])
	}
	return result
})

// DescribeEngine enriches execution metadata for retained clients and prompts.
func DescribeEngine(descriptor process.EngineDescriptor) process.EngineDescriptor {
	descriptor.GuideSHA256 = engineGuideDigests()[descriptor.ID]
	return descriptor
}

func Engines() []process.EngineDescriptor {
	descriptors := process.ExecutionEngines()
	for i := range descriptors {
		descriptors[i] = DescribeEngine(descriptors[i])
	}
	return descriptors
}

func ResolveEngine(id string) (process.EngineDescriptor, error) {
	descriptor, err := process.ResolveExecutionEngine(id)
	if err != nil {
		return process.EngineDescriptor{}, err
	}
	return DescribeEngine(descriptor), nil
}
