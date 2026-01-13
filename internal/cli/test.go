package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Test LLM connectivity",
		Long: `Test that the LLM backend is configured correctly and accessible.

This command sends a simple "Hello, World!" prompt to the configured LLM
and verifies that a response is received. Use this to validate your
API key or Vertex AI setup before running actual commands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("configuration invalid: %w", err)
			}

			fmt.Printf("Testing LLM connectivity...\n")
			fmt.Printf("  Backend: %s\n", cfg.Backend)
			if !cfg.IsCLIBackend() {
				fmt.Printf("  Model: %s\n", cfg.GetModelName())
			}
			fmt.Println()

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Create LLM client
			llmCfg := cfg.GetLLMConfig()
			client, err := llm.NewClient(ctx, llmCfg)
			if err != nil {
				return fmt.Errorf("failed to create LLM client: %w", err)
			}
			defer client.Close()

			// Send a simple test message
			messages := []llm.Message{
				{
					Role:    llm.RoleUser,
					Content: "Say 'Hello from ATR!' and nothing else.",
				},
			}

			start := time.Now()
			resp, err := client.Chat(ctx, messages, nil)
			duration := time.Since(start)

			if err != nil {
				fmt.Printf("LLM test failed: %v\n", err)
				return err
			}

			fmt.Printf("LLM Response: %s\n", resp.Content)
			fmt.Printf("Response time: %s\n", duration.Round(time.Millisecond))
			fmt.Println()
			fmt.Println("LLM connectivity test passed!")

			return nil
		},
	}
}
