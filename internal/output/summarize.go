package output

import (
	"context"
	"fmt"
	"strings"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

const maxOutputForSummary = 4000 // chars to include in summary prompt

// SummarizeOutput uses the LLM to create a one-line summary of command output.
// Uses the configured model (Gemini Flash, Haiku, etc.)
func SummarizeOutput(ctx context.Context, llmClient llm.Client, stdout, stderr string, exitCode int) (string, error) {
	if llmClient == nil {
		return fallbackSummary(exitCode), nil
	}

	// Build the prompt
	prompt := buildSummaryPrompt(stdout, stderr, exitCode)

	// Call LLM without tools (simple chat completion)
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	resp, err := llmClient.Chat(ctx, messages, nil)
	if err != nil {
		// Fall back to basic summary on LLM error
		return fallbackSummary(exitCode), nil
	}

	// Clean up the response
	summary := strings.TrimSpace(resp.Content)

	// Ensure it's a single line and reasonable length
	summary = strings.Split(summary, "\n")[0]
	if len(summary) > 120 {
		summary = summary[:117] + "..."
	}

	return summary, nil
}

// buildSummaryPrompt creates the prompt for the LLM to summarize output.
func buildSummaryPrompt(stdout, stderr string, exitCode int) string {
	var sb strings.Builder

	sb.WriteString(`Summarize this command output in exactly ONE line.

Format guidelines:
- For tests: "✓ X passed in Y.YYs" or "✗ X failed, Y passed in Z.ZZs"
- For builds: "✓ Build succeeded" or "✗ Build failed: [brief reason]"
- For other commands: "✓ Completed successfully" or "✗ Failed: [brief reason]"

Rules:
- Keep it under 80 characters
- Include pass/fail counts and duration if available in the output
- Use ✓ for success (exit code 0), ✗ for failure
- Be concise - no explanations, just the summary line

Command output:
`)

	// Truncate output if too large
	combinedOutput := stdout
	if stderr != "" {
		combinedOutput += "\n--- STDERR ---\n" + stderr
	}

	if len(combinedOutput) > maxOutputForSummary {
		// Include first part and last part
		halfLen := maxOutputForSummary / 2
		combinedOutput = combinedOutput[:halfLen] + "\n...[truncated]...\n" + combinedOutput[len(combinedOutput)-halfLen:]
	}

	sb.WriteString(combinedOutput)
	sb.WriteString(fmt.Sprintf("\n\nExit code: %d\n", exitCode))
	sb.WriteString("\nRespond with ONLY the one-line summary, nothing else.")

	return sb.String()
}

// fallbackSummary provides a basic summary when LLM is unavailable.
func fallbackSummary(exitCode int) string {
	if exitCode == 0 {
		return "✓ Command completed successfully"
	}
	return fmt.Sprintf("✗ Command failed (exit code %d)", exitCode)
}
