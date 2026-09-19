package llm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDesignContextPresentationBoundsAndProviderIsolation(t *testing.T) {
	p := &TranscriptPresentation{Version: 1, DesignContext: &DesignContextPresentation{DesignContextInput: DesignContextInput{ContextAttachmentID: "context", ElementCount: 8, PageURL: strings.Repeat("x", 2048)}, ContextPartIndex: 1}}
	for range 8 {
		p.DesignContext.Elements = append(p.DesignContext.Elements, DesignContextElement{Label: strings.Repeat("x", 160), Selector: strings.Repeat("x", 256)})
	}
	message := Message{Role: "user", Content: "authored", Parts: []ContentPart{{Type: "text", Text: "raw context"}}, Presentation: p}
	plain := message
	plain.Presentation = nil
	stripped, _ := json.Marshal(stripAuthored([]Message{message}))
	baseline, _ := json.Marshal(stripAuthored([]Message{plain}))
	if string(stripped) != string(baseline) || EstimateTokens([]Message{message}) != EstimateTokens([]Message{plain}) {
		t.Fatal("metadata leaked into provider request or accounting")
	}
	for _, limit := range []int{128, 1024, 8192} {
		bounded := BoundPresentation(p, limit)
		wire, _ := json.Marshal(bounded)
		if len(wire) > limit {
			t.Fatalf("presentation exceeds %d: %d", limit, len(wire))
		}
	}
	if len(p.DesignContext.Elements) != 8 || p.DesignContext.PageURL == "" {
		t.Fatal("bounding mutated original")
	}
	wire, _ := json.Marshal(message)
	var restored Message
	if err := json.Unmarshal(wire, &restored); err != nil || !reflect.DeepEqual(restored.Presentation, p) || !reflect.DeepEqual(restored.Parts, message.Parts) {
		t.Fatalf("roundtrip: %+v %v", restored, err)
	}
}
