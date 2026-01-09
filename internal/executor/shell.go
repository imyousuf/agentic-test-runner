package executor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

// shellExecutor implements Executor using the system shell.
type shellExecutor struct {
	shell          string
	commandTimeout time.Duration
	maxOutputSize  int
	envConfig      EnvironmentConfig
}

// Execute runs a command using the system shell.
func (e *shellExecutor) Execute(ctx context.Context, command, cwd string) (*Result, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, e.commandTimeout)
	defer cancel()

	// Detect and apply environment if configured
	finalCommand := command
	var envVars []string

	envs := e.detectEnvironments(cwd)
	if len(envs) > 0 {
		// Apply environment wrapping for the first detected environment of each type
		for _, env := range envs {
			finalCommand = wrapCommandWithEnvironment(finalCommand, env)
		}
		// Build additional environment variables
		envVars = buildEnvironmentVariables(envs)
	}

	// Build the command
	args := shellArgs(finalCommand)
	cmd := exec.CommandContext(ctx, e.shell, args...)
	cmd.Dir = cwd

	// Set environment variables
	if len(envVars) > 0 {
		cmd.Env = append(os.Environ(), envVars...)
	}

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

// detectEnvironments detects environments based on configuration.
func (e *shellExecutor) detectEnvironments(cwd string) []*DetectedEnvironment {
	var envs []*DetectedEnvironment

	// Manual Python configuration takes precedence
	if e.envConfig.PythonVenvPath != "" {
		activatePath := getActivatePath(e.envConfig.PythonVenvPath)
		envs = append(envs, &DetectedEnvironment{
			Type:         EnvTypePythonVenv,
			Path:         e.envConfig.PythonVenvPath,
			ActivatePath: activatePath,
		})
	} else if e.envConfig.CondaEnvName != "" {
		envs = append(envs, &DetectedEnvironment{
			Type: EnvTypeConda,
			Path: e.envConfig.CondaEnvName,
		})
	} else if !e.envConfig.DisablePythonEnv && e.envConfig.AutoDetect {
		if pyEnv := detectPythonEnv(cwd); pyEnv != nil {
			envs = append(envs, pyEnv)
		}
	}

	// Manual Node configuration takes precedence
	if e.envConfig.NodeVersion != "" {
		envs = append(envs, &DetectedEnvironment{
			Type:    EnvTypeNVM,
			Version: e.envConfig.NodeVersion,
		})
	} else if !e.envConfig.DisableNodeEnv && e.envConfig.AutoDetect {
		if nodeEnv := detectNodeEnv(cwd); nodeEnv != nil {
			envs = append(envs, nodeEnv)
		}
	}

	return envs
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
