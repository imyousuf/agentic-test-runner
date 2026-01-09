//go:build windows

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
		if strings.HasSuffix(env.ActivatePath, ".ps1") {
			// PowerShell: use . (dot-source) to activate
			return fmt.Sprintf(". '%s'; %s", env.ActivatePath, command)
		}
		// CMD: use call to run activate.bat
		return fmt.Sprintf("& '%s'; %s", env.ActivatePath, command)

	case EnvTypeConda:
		envName := filepath.Base(env.Path)
		return fmt.Sprintf("conda activate %s; %s", envName, command)

	case EnvTypeNVM:
		// nvm-windows uses different commands
		return fmt.Sprintf("nvm use %s; %s", env.Version, command)

	case EnvTypeFNM:
		// fnm on Windows
		return fmt.Sprintf("fnm use %s; %s", env.Version, command)

	case EnvTypeNodeModules:
		// Prepend node_modules/.bin to PATH in PowerShell
		return fmt.Sprintf("$env:PATH = '%s;' + $env:PATH; %s", env.Path, command)
	}

	return command
}

// buildEnvironmentVariables builds environment variables for the detected environments.
func buildEnvironmentVariables(envs []*DetectedEnvironment) []string {
	var result []string

	for _, env := range envs {
		switch env.Type {
		case EnvTypePythonVenv:
			result = append(result, fmt.Sprintf("VIRTUAL_ENV=%s", env.Path))
			scriptsPath := filepath.Join(env.Path, "Scripts")
			if currentPath := os.Getenv("PATH"); currentPath != "" {
				result = append(result, fmt.Sprintf("PATH=%s;%s", scriptsPath, currentPath))
			}

		case EnvTypeNodeModules:
			if currentPath := os.Getenv("PATH"); currentPath != "" {
				if !strings.Contains(currentPath, env.Path) {
					result = append(result, fmt.Sprintf("PATH=%s;%s", env.Path, currentPath))
				}
			}
		}
	}

	return result
}
