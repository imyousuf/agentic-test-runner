//go:build linux || darwin

package executor

import (
	"os"
	"path/filepath"
	"strings"
)

// detectPythonEnv detects Python virtual environments.
func detectPythonEnv(cwd string) *DetectedEnvironment {
	// Check common venv directory names in order of preference
	venvDirs := []string{".venv", "venv", "env", ".env"}

	// Search current directory and parent directories
	dir := cwd
	for {
		for _, venvDir := range venvDirs {
			venvPath := filepath.Join(dir, venvDir)
			activatePath := filepath.Join(venvPath, "bin", "activate")

			if _, err := os.Stat(activatePath); err == nil {
				return &DetectedEnvironment{
					Type:         EnvTypePythonVenv,
					Path:         venvPath,
					ActivatePath: activatePath,
				}
			}
		}

		// Check for conda environment marker (conda-meta directory)
		condaMeta := filepath.Join(dir, "conda-meta")
		if info, err := os.Stat(condaMeta); err == nil && info.IsDir() {
			return &DetectedEnvironment{
				Type: EnvTypeConda,
				Path: dir,
			}
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break // Reached root
		}
		dir = parent
	}

	return nil
}

// detectNodeEnv detects Node.js environment configuration.
func detectNodeEnv(cwd string) *DetectedEnvironment {
	dir := cwd
	for {
		// Check for .nvmrc file
		nvmrcPath := filepath.Join(dir, ".nvmrc")
		if content, err := os.ReadFile(nvmrcPath); err == nil {
			version := strings.TrimSpace(string(content))
			return &DetectedEnvironment{
				Type:    EnvTypeNVM,
				Path:    nvmrcPath,
				Version: version,
			}
		}

		// Check for .node-version file (used by fnm and other version managers)
		nodeVersionPath := filepath.Join(dir, ".node-version")
		if content, err := os.ReadFile(nodeVersionPath); err == nil {
			version := strings.TrimSpace(string(content))
			return &DetectedEnvironment{
				Type:    EnvTypeFNM,
				Path:    nodeVersionPath,
				Version: version,
			}
		}

		// Check for node_modules/.bin directory
		nodeModulesBin := filepath.Join(dir, "node_modules", ".bin")
		if info, err := os.Stat(nodeModulesBin); err == nil && info.IsDir() {
			return &DetectedEnvironment{
				Type: EnvTypeNodeModules,
				Path: nodeModulesBin,
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil
}

// getActivatePath returns the activate script path for Unix systems.
func getActivatePath(venvPath string) string {
	return filepath.Join(venvPath, "bin", "activate")
}
