package tui

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/computer"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestComputerUseCommand(t *testing.T) {
	services := tools.NewServices()
	policy := computer.NewPolicy([]string{"Google Chrome"}, nil, true)
	services.SetComputerPolicy(policy)
	m := &model{agent: agent.NewWithServices(llm.New("http://unused", "k"), "m", 1, "sys", services)}

	// status
	m.computerUseCommand(nil, "/computer-use")
	if last := lastBlock(m); !strings.Contains(last, "computer-use") {
		t.Fatalf("status: %q", last)
	}
	// allow
	if computer.Available() { // platform-gated paths run on mac only
		m.computerUseCommand([]string{"allow", "Safari"}, "/computer-use allow Safari")
		if err := policy.Check("Safari"); err != nil {
			t.Fatalf("allow must unblock: %v", err)
		}
		// deny wins
		m.computerUseCommand([]string{"deny", "Safari"}, "/computer-use deny Safari")
		if err := policy.Check("Safari"); err == nil {
			t.Fatal("deny must re-block")
		}
	}
}

// The task form wraps the task with the computer-use steer.
func TestComputerUseTaskSubmits(t *testing.T) {
	msg := computerUseInstruction("check my calendar")
	if !strings.Contains(msg, "computer_exec") || !strings.Contains(msg, "check my calendar") {
		t.Fatalf("instruction must carry the tool steer + task, got %.100q", msg)
	}
}
