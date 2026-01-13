// Package main is the entry point for the atr CLI.
package main

import (
	"fmt"
	"os"

	"github.com/imyousuf/agentic-test-runner/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
