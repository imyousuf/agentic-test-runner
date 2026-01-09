//go:build linux || darwin

package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// wrapCommandWithEnvironment wraps a command to activate the detected environment.
func wrapCommandWithEnvironment(command string, env *DetectedEnvironment) string {
	if env == nil {
		return command
	}

	switch env.Type {
	case EnvTypePythonVenv:
		// Source the activate script before running the command
		return fmt.Sprintf("source %s && %s", env.ActivatePath, command)

	case EnvTypeConda:
		// Use conda activate
		envName := filepath.Base(env.Path)
		return fmt.Sprintf("conda activate %s && %s", envName, command)

	case EnvTypeNVM:
		// Source nvm and use the specified version
		nvmDir := os.Getenv("NVM_DIR")
		if nvmDir == "" {
			nvmDir = filepath.Join(os.Getenv("HOME"), ".nvm")
		}
		nvmScript := filepath.Join(nvmDir, "nvm.sh")
		return fmt.Sprintf("source %s && nvm use %s && %s", nvmScript, env.Version, command)

	case EnvTypeFNM:
		// Use fnm to set the Node version
		return fmt.Sprintf("eval \"$(fnm env)\" && fnm use %s && %s", env.Version, command)

	case EnvTypeNodeModules:
		// Prepend node_modules/.bin to PATH
		return fmt.Sprintf("export PATH=\"%s:$PATH\" && %s", env.Path, command)
	}

	return command
}

// buildEnvironmentVariables builds environment variables for the detected environments.
func buildEnvironmentVariables(envs []*DetectedEnvironment) []string {
	var result []string

	for _, env := range envs {
		switch env.Type {
		case EnvTypePythonVenv:
			// Set VIRTUAL_ENV environment variable
			result = append(result, fmt.Sprintf("VIRTUAL_ENV=%s", env.Path))
			// Prepend venv bin to PATH
			binPath := filepath.Join(env.Path, "bin")
			if currentPath := os.Getenv("PATH"); currentPath != "" {
				result = append(result, fmt.Sprintf("PATH=%s:%s", binPath, currentPath))
			}

		case EnvTypeNodeModules:
			// Prepend node_modules/.bin to PATH
			if currentPath := os.Getenv("PATH"); currentPath != "" {
				if !strings.Contains(currentPath, env.Path) {
					result = append(result, fmt.Sprintf("PATH=%s:%s", env.Path, currentPath))
				}
			}
		}
	}

	return result
}
