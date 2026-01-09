package executor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// shellExecutor implements Executor using the system shell.
type shellExecutor struct {
	shell          string
	commandTimeout time.Duration
	maxOutputSize  int
	envConfig      EnvironmentConfig
	llmClient      llm.Client
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
		// Determine which environments are actually needed for this command
		needs := e.detectEnvironmentNeeds(ctx, command)
		if needs != nil {
			// Filter to only needed environments
			envs = filterEnvironments(envs, needs)
		}
		// If needs is nil (unknown), use all detected environments (backward compatible)

		// Apply environment wrapping for the filtered environments
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

// detectEnvironmentNeeds determines which environments a command needs.
// It tries LLM detection first (if enabled and available), then falls back to pattern matching.
// Returns nil if the command's needs cannot be determined (will use all detected environments).
func (e *shellExecutor) detectEnvironmentNeeds(ctx context.Context, command string) *EnvironmentNeeds {
	// Strategy 1: Try LLM if available and enabled
	if e.envConfig.UseLLMDetection && e.llmClient != nil {
		needs, err := DetectEnvironmentNeeds(ctx, e.llmClient, command)
		if err == nil {
			return needs
		}
		// LLM failed, fall through to pattern matching
	}

	// Strategy 2: Pattern matching fallback
	return DetectEnvironmentNeedsFromPattern(command)
}

// TestEnvironmentDetection returns what environments would be used for a command
// without actually executing it. Used by the test-cmd-env command.
func TestEnvironmentDetection(ctx context.Context, command, cwd string,
	envConfig EnvironmentConfig, llmClient llm.Client) *EnvironmentTestResult {

	result := &EnvironmentTestResult{
		Command: command,
		Cwd:     cwd,
	}

	// Create a temporary executor to use detection methods
	exec := &shellExecutor{
		envConfig: envConfig,
		llmClient: llmClient,
	}

	// Detect available environments
	result.DetectedEnvs = exec.detectEnvironments(cwd)

	// Determine needs (same logic as Execute)
	if envConfig.UseLLMDetection && llmClient != nil {
		needs, err := DetectEnvironmentNeeds(ctx, llmClient, command)
		if err == nil {
			result.Needs = needs
			result.DetectionMethod = "LLM"
		}
	}

	if result.Needs == nil {
		result.Needs = DetectEnvironmentNeedsFromPattern(command)
		if result.Needs != nil {
			result.DetectionMethod = "Pattern matching"
		} else {
			result.DetectionMethod = "Unknown"
		}
	}

	// Filter environments based on needs
	if result.Needs != nil {
		result.ActiveEnvs = filterEnvironments(result.DetectedEnvs, result.Needs)
	} else {
		// Unknown needs - use all detected environments
		result.ActiveEnvs = result.DetectedEnvs
	}

	// Build final command preview
	finalCommand := command
	for _, env := range result.ActiveEnvs {
		finalCommand = wrapCommandWithEnvironment(finalCommand, env)
	}
	result.FinalCommand = finalCommand

	return result
}
