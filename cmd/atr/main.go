// Package main is the entry point for the atr CLI.
package main

import (
	"os"

	"github.com/imyousuf/agentic-test-runner/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
