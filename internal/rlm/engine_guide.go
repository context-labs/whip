package rlm

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

var engineGuideDigests = sync.OnceValue(func() map[string]string {
	result := make(map[string]string)
	for _, descriptor := range ExecutionEngines() {
		guide, _ := RuntimeGuide(descriptor.ID, ModuleNames(), nil, nil, "", nil)
		digest := sha256.Sum256([]byte(guide))
		result[descriptor.ID] = hex.EncodeToString(digest[:])
	}
	return result
})

// DescribeEngine enriches execution metadata for retained clients and prompts.
func DescribeEngine(descriptor EngineDescriptor) EngineDescriptor {
	descriptor.GuideSHA256 = engineGuideDigests()[descriptor.ID]
	return descriptor
}

func Engines() []EngineDescriptor {
	descriptors := ExecutionEngines()
	for i := range descriptors {
		descriptors[i] = DescribeEngine(descriptors[i])
	}
	return descriptors
}

func ResolveEngine(id string) (EngineDescriptor, error) {
	descriptor, err := ResolveExecutionEngine(id)
	if err != nil {
		return EngineDescriptor{}, err
	}
	return DescribeEngine(descriptor), nil
}
