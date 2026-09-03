// The question tool: the model pauses the turn and asks the user to pick from
// a short list of options (or type their own answer). Same shape as
// opencode's question tool, cut to one question per call. The TUI installs
// Ask; without it (whip run, tests) the tool tells the model no one is there.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

// AskOption is one selectable answer.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskRequest is one question for the user.
type AskRequest struct {
	Question string      `json:"question"`
	Options  []AskOption `json:"options"`
	Multiple bool        `json:"multiple"`
}

// Ask is the installed UI hook. It returns the chosen labels (or the typed
// custom answer) and ok=false when the user dismissed the question or ctx was
// cancelled. Nil = no interactive user.
var Ask func(ctx context.Context, req AskRequest) (answers []string, ok bool)

// QuestionTool is registered for the main agent only — subagents are told not
// to ask questions and the MCP server has no user to ask.
func QuestionTool() Tool {
	return Tool{
		Def: llm.NewTool("question",
			"Ask the user to choose between options when a decision is genuinely theirs: ambiguous instructions, a preference, or a fork in implementation. The user sees a selectable list; a \"type your own answer\" row is always added, so never include an \"Other\" option. If you recommend an option, put it first and end its label with \"(Recommended)\". Answers come back as the chosen labels.",
			`{"type":"object","properties":{"question":{"type":"string","description":"The complete question"},"options":{"type":"array","minItems":2,"maxItems":6,"items":{"type":"object","properties":{"label":{"type":"string","description":"Display text, 1-5 words"},"description":{"type":"string","description":"One line explaining the choice"}},"required":["label"]}},"multiple":{"type":"boolean","description":"Allow selecting more than one option"}},"required":["question","options"]}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a AskRequest
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			if Ask == nil {
				return "", errors.New("no interactive user to ask; make a reasonable assumption and continue")
			}
			answers, ok := Ask(ctx, a)
			if !ok {
				return "", errors.New("the user dismissed the question")
			}
			return "User answered \"" + a.Question + "\": " + strings.Join(answers, ", "), nil
		},
	}
}
