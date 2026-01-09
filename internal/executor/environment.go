// Package executor provides shell command execution functionality.
package executor

// EnvironmentType represents the type of development environment.
type EnvironmentType string

const (
	// EnvTypePythonVenv represents a Python virtual environment (venv/virtualenv).
	EnvTypePythonVenv EnvironmentType = "python-venv"
	// EnvTypeConda represents a Conda environment.
	EnvTypeConda EnvironmentType = "conda"
	// EnvTypeNVM represents a Node version managed by nvm.
	EnvTypeNVM EnvironmentType = "nvm"
	// EnvTypeFNM represents a Node version managed by fnm.
	EnvTypeFNM EnvironmentType = "fnm"
	// EnvTypeNodeModules represents a local node_modules/.bin directory.
	EnvTypeNodeModules EnvironmentType = "node-modules"
)

// DetectedEnvironment represents a detected development environment.
type DetectedEnvironment struct {
	// Type is the environment type.
	Type EnvironmentType
	// Path is the absolute path to the environment.
	Path string
	// ActivatePath is the path to the activation script (for Python venvs).
	ActivatePath string
	// Version is the version file content (for .nvmrc, etc.).
	Version string
}

// EnvironmentConfig holds configuration for environment detection and activation.
type EnvironmentConfig struct {
	// AutoDetect enables automatic environment detection (default: true).
	AutoDetect bool
	// PythonVenvPath manually specifies a Python virtual environment path.
	PythonVenvPath string
	// CondaEnvName manually specifies a conda environment name.
	CondaEnvName string
	// NodeVersion manually specifies a Node.js version for nvm/fnm.
	NodeVersion string
	// DisablePythonEnv disables Python environment detection.
	DisablePythonEnv bool
	// DisableNodeEnv disables Node.js environment detection.
	DisableNodeEnv bool
}
