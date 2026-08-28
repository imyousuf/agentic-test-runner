// Package main is the entry point for the atr CLI.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/imyousuf/agentic-test-runner/internal/cli"
)

func main() {
	err := cli.Execute()
	if err == nil {
		return
	}

	// A command that wants a particular exit code says so rather than calling
	// os.Exit itself, which would skip its own deferred cleanup.
	var exit *cli.ExitError
	if errors.As(err, &exit) {
		if exit.Err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", exit.Err)
		}
		os.Exit(exit.Code)
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
