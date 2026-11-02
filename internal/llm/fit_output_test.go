package llm

import (
	"strings"
	"testing"
)

// Providers count the output reservation against the same window as the
// prompt, so an output cap equal to the whole window is rejected once any
// prompt sits beside it (#345). FitOutput keeps input plus output inside it.
func TestFitOutputKeepsInputPlusOutputInsideTheWindow(t *testing.T) {
	t.Parallel()
	client := New("http://example.invalid", "key")
	request := Request{
		Model:     "model",
		Messages:  []Message{{Role: "user", Content: strings.Repeat("abcd", 1000)}},
		Tools:     []Tool{NewTool("echo", "echo", `{"type":"object"}`)},
		MaxTokens: 10_000,
	}
	input, err := requestTokens(request)
	if err != nil {
		t.Fatal(err)
	}
	// fitted is the cap FitOutput should pick for a window with room left
	// beside the given input: the room minus the undercount reserve.
	fitted := func(limit, input int) int {
		room := limit - input
		return room - min(max(input/fitOutputReserveDivisor, fitOutputReserveFloor), room/2)
	}
	cases := []struct {
		name                 string
		limit, reported, cap int
		want                 int
	}{
		{name: "window fits the cap", limit: 100_000, cap: 10_000, want: 10_000},
		{name: "cap fills the window", limit: 10_000, cap: 10_000, want: fitted(10_000, int(input))},
		{name: "reported prompt is a floor", limit: 10_000, reported: 6_000, cap: 10_000, want: fitted(10_000, 6_000)},
		{name: "smaller report than estimate keeps estimate", limit: 10_000, reported: 1, cap: 10_000, want: fitted(10_000, int(input))},
		{name: "unknown window", limit: 0, cap: 10_000, want: 10_000},
		{name: "omitted cap", limit: 10_000, cap: 0, want: 0},
		{name: "reserve keeps at least half the room", limit: int(input) + 100, cap: 10_000, want: 50},
		{name: "one token of room", limit: int(input) + 1, cap: 10_000, want: 1},
		{name: "input fills the window", limit: int(input), cap: 10_000, want: 10_000},
		{name: "reported input overflows", limit: 10_000, reported: 20_000, cap: 10_000, want: 10_000},
	}
	for _, tc := range cases {
		request := request
		request.MaxTokens = tc.cap
		if got := client.FitOutput(request, tc.limit, tc.reported); got != tc.want {
			t.Errorf("%s: FitOutput = %d, want %d (input estimate %d)", tc.name, got, tc.want, input)
		}
	}
}

// The first request of a session has only the chars/4 estimate, which
// undercounts (images by ~7x). A request whose real size is somewhat above
// the estimate must still fit input plus output inside the window.
func TestFitOutputLeavesHeadroomForAnUndercountedEstimate(t *testing.T) {
	t.Parallel()
	client := New("http://example.invalid", "key")
	for _, chars := range []int{4_000, 400_000} {
		request := Request{
			Model:     "model",
			Messages:  []Message{{Role: "user", Content: strings.Repeat("abcd", chars/4)}},
			MaxTokens: 262_144,
		}
		estimate, err := requestTokens(request)
		if err != nil {
			t.Fatal(err)
		}
		actual := int(estimate) + int(estimate)/20 + 1 // 5% undercount
		got := client.FitOutput(request, 262_144, 0)
		if got <= 0 || actual+got > 262_144 {
			t.Fatalf("estimate %d (actual %d): FitOutput = %d overflows the 262144 window", estimate, actual, got)
		}
	}
}

// The ChatGPT subscription transport cannot enforce smaller caps; its natural
// ceiling replaces the request cap, so FitOutput must not lower it.
func TestFitOutputLeavesNaturalOutputLimitsAlone(t *testing.T) {
	t.Parallel()
	client, _ := subscriptionTestClient(t)
	request := Request{Model: "gpt-5.5", Messages: []Message{{Role: "user", Content: "hello"}}, MaxTokens: 128000}
	if got := client.FitOutput(request, 128000, 0); got != 128000 {
		t.Fatalf("subscription FitOutput = %d, want natural limit unchanged", got)
	}
}
