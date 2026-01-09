//go:build windows

package executor

import (
	"os"
	"path/filepath"
	"strings"
)

// detectPythonEnv detects Python virtual environments on Windows.
func detectPythonEnv(cwd string) *DetectedEnvironment {
	venvDirs := []string{".venv", "venv", "env", ".env"}

	dir := cwd
	for {
		for _, venvDir := range venvDirs {
			venvPath := filepath.Join(dir, venvDir)
			// Windows uses Scripts/activate.bat or Scripts/Activate.ps1
			activatePs1 := filepath.Join(venvPath, "Scripts", "Activate.ps1")
			activateBat := filepath.Join(venvPath, "Scripts", "activate.bat")

			if _, err := os.Stat(activatePs1); err == nil {
				return &DetectedEnvironment{
					Type:         EnvTypePythonVenv,
					Path:         venvPath,
					ActivatePath: activatePs1,
				}
			}
			if _, err := os.Stat(activateBat); err == nil {
				return &DetectedEnvironment{
					Type:         EnvTypePythonVenv,
					Path:         venvPath,
					ActivatePath: activateBat,
				}
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

// detectNodeEnv detects Node.js environment configuration on Windows.
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

		// Check for .node-version file
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

// getActivatePath returns the activate script path for Windows systems.
func getActivatePath(venvPath string) string {
	// Prefer PowerShell activation script
	ps1Path := filepath.Join(venvPath, "Scripts", "Activate.ps1")
	if _, err := os.Stat(ps1Path); err == nil {
		return ps1Path
	}
	return filepath.Join(venvPath, "Scripts", "activate.bat")
}
