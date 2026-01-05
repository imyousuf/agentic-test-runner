package executor

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// shellExecutor implements Executor using the system shell.
type shellExecutor struct {
	shell          string
	commandTimeout time.Duration
	maxOutputSize  int
}

// Execute runs a command using the system shell.
func (e *shellExecutor) Execute(ctx context.Context, command, cwd string) (*Result, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, e.commandTimeout)
	defer cancel()

	// Build the command
	args := shellArgs(command)
	cmd := exec.CommandContext(ctx, e.shell, args...)
	cmd.Dir = cwd

	// Capture output with size limits
	var stdout, stderr limitedBuffer
	stdout.limit = e.maxOutputSize
	stderr.limit = e.maxOutputSize
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute
	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Command:  command,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	// Handle timeout
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}

	// Extract exit code
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err
			result.ExitCode = -1
		}
	}

	return result, nil
}

// Shell returns the shell being used.
func (e *shellExecutor) Shell() string {
	return e.shell
}

// limitedBuffer is a buffer that stops accepting data after reaching a limit.
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (n int, err error) {
	if b.buf.Len() >= b.limit {
		b.truncated = true
		return len(p), nil // Pretend we wrote it
	}

	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}

	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	s := b.buf.String()
	if b.truncated {
		s += "\n... [output truncated]"
	}
	return s
}
