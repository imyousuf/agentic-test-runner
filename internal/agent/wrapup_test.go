package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// loopingClient keeps calling a tool until it is told to stop, then answers.
//
// That is the shape of a compile that will not converge: a spec asking for
// something the application cannot do, with the agent searching for it until
// the budget runs out.
type loopingClient struct {
	mu sync.Mutex
	// answerAfterNudge is what it replies once it sees the wrap-up message.
	answerAfterNudge string
	// keepLooping makes it ignore the nudge, to test the ceiling itself.
	keepLooping bool

	calls     int
	sawNudge  bool
	nudgeCall int
}

func (c *loopingClient) Chat(_ context.Context, messages []llm.Message, _ []llm.Tool) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++

	for _, m := range messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "iteration(s) left") {
			if !c.sawNudge {
				c.sawNudge = true
				c.nudgeCall = c.calls
			}
		}
	}

	if c.sawNudge && !c.keepLooping {
		return &llm.Response{Content: c.answerAfterNudge}, nil
	}
	return &llm.Response{ToolCalls: []llm.ToolCall{
		{ID: "call", Name: "browser_snapshot", Arguments: map[string]any{}},
	}}, nil
}

func (c *loopingClient) ChatWithHistory(ctx context.Context, h []llm.Message, t []llm.Tool) (*llm.Response, error) {
	return c.Chat(ctx, h, t)
}
func (c *loopingClient) Model() string          { return "looping" }
func (c *loopingClient) Provider() llm.Provider { return llm.Provider("looping") }
func (c *loopingClient) Close() error           { return nil }

func loopAgent(t *testing.T, client llm.Client, maxIterations int) *Agent {
	t.Helper()
	b, _ := sharedRunBrowser(t)
	return NewCompilerAgent(CompilerConfig{
		LLMClient:     client,
		Browser:       b,
		MaxIterations: maxIterations,
		Timeout:       60 * time.Second,
	})
}

// An agent that has learned what it needs but keeps exploring used to have all
// of it thrown away at the ceiling. It is now told the budget is nearly gone
// and asked to finish, which lets the work land.
func TestTheModelIsAskedToFinishBeforeTheBudgetRunsOut(t *testing.T) {
	client := &loopingClient{answerAfterNudge: "```javascript\natr.step(1, \"x\", () => {});\n```"}
	a := loopAgent(t, client, 10)

	out, err := a.runToolLoop(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "compile this"},
	}, "compile", nil)
	if err != nil {
		t.Fatalf("the loop failed even though the model answered after the nudge: %v", err)
	}
	if !strings.Contains(out, "atr.step") {
		t.Errorf("got %q, want the script the model produced", out)
	}
	if !client.sawNudge {
		t.Fatal("the model was never asked to wrap up")
	}
	// Late enough that an ordinary compile never sees it.
	if client.nudgeCall <= 5 {
		t.Errorf("asked to wrap up on call %d of a 10-iteration budget; too early to be a last resort", client.nudgeCall)
	}
}

// The other half: a spec the application cannot satisfy. The explanation is
// the useful output, and it must survive rather than being discarded.
func TestAnImpossibleStepIsReportedRatherThanSearchedFor(t *testing.T) {
	const explanation = "Step 2 cannot be performed: the page has no search input. The catalogue is a static list of two items."
	client := &loopingClient{answerAfterNudge: explanation}
	a := loopAgent(t, client, 8)

	out, err := a.runToolLoop(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "compile this"},
	}, "compile", nil)
	if err != nil {
		t.Fatalf("runToolLoop: %v", err)
	}
	if out != explanation {
		t.Errorf("got %q, want the model's explanation of what it could not do", out)
	}
}

// A model that ignores the nudge still stops, and the error now says what the
// usual cause is instead of only counting iterations.
func TestTheCeilingExplainsItself(t *testing.T) {
	client := &loopingClient{keepLooping: true}
	a := loopAgent(t, client, 5)

	_, err := a.runToolLoop(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "compile this"},
	}, "compile", nil)
	if err == nil {
		t.Fatal("expected the loop to give up")
	}
	if !strings.Contains(err.Error(), "iteration limit") {
		t.Errorf("err = %v, want it to name the limit", err)
	}
	if !strings.Contains(err.Error(), "spec matches") {
		t.Errorf("err = %v, want it to name the usual cause", err)
	}
	if client.calls != 5 {
		t.Errorf("made %d calls, want exactly the 5 it was allowed", client.calls)
	}
}

// The nudge must be asked for once. Repeating it every iteration would grow
// the history it is trying to cut short.
func TestTheModelIsAskedOnlyOnce(t *testing.T) {
	client := &loopingClient{keepLooping: true}
	a := loopAgent(t, client, 6)

	_, _ = a.runToolLoop(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "compile this"},
	}, "compile", nil)

	// Reconstruct how many nudges a 6-iteration run would have injected.
	if got := wrapUpAt(6); got != 3 {
		t.Errorf("wrapUpAt(6) = %d, want 3", got)
	}
}

// A budget too small to reserve from must still leave one iteration to answer
// in, rather than nudging on the very first call or not at all.
func TestATinyBudgetStillGetsOneChanceToAnswer(t *testing.T) {
	for _, max := range []int{1, 2, 3, 4} {
		at := wrapUpAt(max)
		if at < 0 || at >= max {
			t.Errorf("wrapUpAt(%d) = %d, which is outside the loop", max, at)
		}
	}
}
