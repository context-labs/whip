//go:build integration

package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
)

func TestRuntimeNumericContentArguments(t *testing.T) {
	for _, engine := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			_, _, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1, engine)
			value, err := runtime.rootNode.host.Call(t.Context(), "state", "private_set", map[string]any{
				"key": "pages", "value": strings.Repeat("a", 9000) + strings.Repeat("b", 9000),
			})
			if err != nil {
				t.Fatal(err)
			}
			handle := value.(map[string]any)["handle"].(string)
			code := fmt.Sprintf("chunk = context.read(handle=%q, offset=9001, length=3)\nprint(chunk[\"text\"], chunk[\"span\"][\"start\"], chunk[\"span\"][\"end\"])\ncontext.history(limit=1)\n", handle)
			if engine == rlm.EngineQuickJS {
				code = fmt.Sprintf(`var chunk = await context.read({handle:%q, offset:9001, length:3}); print(chunk.text, chunk.span.start, chunk.span.end); await context.history({limit:1});`, handle)
			}
			result, err := runtime.rootNode.kernel.Exec(t.Context(), code)
			if err != nil || result.Output != "bbb 9001 9004\n" {
				t.Fatalf("numeric content arguments: output=%.120q err=%v", result.Output, err)
			}
		})
	}
}
