// Package config handles configuration loading and validation for ATR.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

const (
	// DefaultConfigDir is the default configuration directory.
	DefaultConfigDir = ".atr"
	// DefaultConfigFile is the default configuration file name.
	DefaultConfigFile = "config"
	// DefaultConfigType is the default configuration file type.
	DefaultConfigType = "yaml"
)

// Config holds all configuration for ATR.
type Config struct {
	// Backend specifies which LLM backend to use.
	Backend string `mapstructure:"backend"`

	// Gemini contains Gemini API configuration.
	Gemini GeminiConfig `mapstructure:"gemini"`

	// Vertex contains Vertex AI configuration.
	Vertex VertexConfig `mapstructure:"vertex"`

	// Model specifies which model tier to use (flash or pro).
	Model string `mapstructure:"model"`

	// Models contains model name mappings.
	Models ModelsConfig `mapstructure:"models"`

	// Agent contains agent behavior configuration.
	Agent AgentConfig `mapstructure:"agent"`

	// Executor contains command execution configuration.
	Executor ExecutorConfig `mapstructure:"executor"`

	// Behavior contains browser behavior testing configuration.
	Behavior BehaviorConfig `mapstructure:"behavior"`

	// Server contains browser server configuration.
	Server ServerConfig `mapstructure:"server"`

	// Update contains update configuration.
	Update UpdateConfig `mapstructure:"update"`
}

// GeminiConfig holds Gemini API configuration.
type GeminiConfig struct {
	// APIKey is the Gemini API key.
	APIKey string `mapstructure:"api_key"`
}

// VertexConfig holds Vertex AI configuration.
type VertexConfig struct {
	// Project is the GCP project ID.
	Project string `mapstructure:"project"`
	// Location is the GCP region.
	Location string `mapstructure:"location"`
	// CredentialsFile is the path to a service account JSON key file.
	CredentialsFile string `mapstructure:"credentials_file"`
}

// ModelsConfig holds model name mappings.
type ModelsConfig struct {
	// Flash is the model name for the flash tier.
	Flash string `mapstructure:"flash"`
	// Pro is the model name for the pro tier.
	Pro string `mapstructure:"pro"`
}

// AgentConfig holds agent behavior configuration.
type AgentConfig struct {
	// MaxIterations is the maximum number of agent loop iterations.
	MaxIterations int `mapstructure:"max_iterations"`
	// Timeout is the maximum time for the entire agent analysis.
	Timeout time.Duration `mapstructure:"timeout"`
	// Temperature controls LLM response randomness.
	Temperature float32 `mapstructure:"temperature"`
}

// ExecutorConfig holds command execution configuration.
type ExecutorConfig struct {
	// CommandTimeout is the maximum time for a single command.
	CommandTimeout time.Duration `mapstructure:"command_timeout"`
	// MaxOutputSize is the maximum bytes to capture from command output.
	MaxOutputSize int `mapstructure:"max_output_size"`
	// Environment contains environment detection configuration.
	Environment ExecutorEnvironmentConfig `mapstructure:"environment"`
}

// ExecutorEnvironmentConfig holds configuration for environment detection and activation.
type ExecutorEnvironmentConfig struct {
	// AutoDetect enables automatic environment detection (default: true).
	AutoDetect bool `mapstructure:"auto_detect"`
	// PythonVenvPath manually specifies a Python virtual environment path.
	PythonVenvPath string `mapstructure:"python_venv_path"`
	// CondaEnvName manually specifies a conda environment name.
	CondaEnvName string `mapstructure:"conda_env_name"`
	// NodeVersion manually specifies a Node.js version for nvm/fnm.
	NodeVersion string `mapstructure:"node_version"`
	// DisablePythonEnv disables Python environment detection.
	DisablePythonEnv bool `mapstructure:"disable_python_env"`
	// DisableNodeEnv disables Node.js environment detection.
	DisableNodeEnv bool `mapstructure:"disable_node_env"`
	// UseLLMDetection uses LLM to analyze commands and determine environment needs (default: true).
	UseLLMDetection bool `mapstructure:"use_llm_detection"`
}

// BehaviorConfig holds browser behavior testing configuration.
type BehaviorConfig struct {
	// Browser contains browser settings.
	Browser BrowserConfig `mapstructure:"browser"`
	// Capture contains failure context capture settings.
	Capture CaptureConfig `mapstructure:"capture"`
	// BaseURL is the base URL for relative navigation.
	BaseURL string `mapstructure:"base_url"`
}

// BrowserConfig holds browser settings for behavior testing.
type BrowserConfig struct {
	// Executable is the path to browser binary ("auto" for rod's auto-download).
	Executable string `mapstructure:"executable"`
	// CacheDir is the directory for rod's browser cache.
	CacheDir string `mapstructure:"cache_dir"`
	// Headless runs browser in headless mode.
	Headless bool `mapstructure:"headless"`
	// IgnoreHTTPSErrors ignores SSL/TLS certificate errors (useful for local dev with self-signed certs).
	IgnoreHTTPSErrors bool `mapstructure:"ignore_https_errors"`
	// Viewport contains viewport dimensions.
	Viewport ViewportConfig `mapstructure:"viewport"`
	// PageTimeout is the default page load timeout.
	PageTimeout time.Duration `mapstructure:"page_timeout"`
	// ActionTimeout is the default timeout for actions (click, type, etc.).
	ActionTimeout time.Duration `mapstructure:"action_timeout"`
	// SlowMotion adds delay between actions for debugging (0 = disabled).
	SlowMotion time.Duration `mapstructure:"slow_motion"`
}

// ViewportConfig holds viewport dimensions.
type ViewportConfig struct {
	// Width is the viewport width in pixels.
	Width int `mapstructure:"width"`
	// Height is the viewport height in pixels.
	Height int `mapstructure:"height"`
}

// CaptureConfig holds failure context capture settings.
type CaptureConfig struct {
	// Screenshots enables screenshot capture on failure.
	Screenshots bool `mapstructure:"screenshots"`
	// FullPageScreenshot captures entire scrollable page.
	FullPageScreenshot bool `mapstructure:"full_page_screenshot"`
	// ConsoleLogs enables console log capture.
	ConsoleLogs bool `mapstructure:"console_logs"`
	// NetworkHAR enables network request capture.
	NetworkHAR bool `mapstructure:"network_har"`
	// DOMSnapshot enables DOM snapshot capture.
	DOMSnapshot bool `mapstructure:"dom_snapshot"`
	// MaxConsoleEntries limits console entries captured.
	MaxConsoleEntries int `mapstructure:"max_console_entries"`
	// MaxNetworkRequests limits network requests captured.
	MaxNetworkRequests int `mapstructure:"max_network_requests"`
}

// ServerConfig holds browser server configuration.
type ServerConfig struct {
	// Port is the HTTP server port (0 for auto-select, default: 9333).
	Port int `mapstructure:"port"`
	// ReadTimeout is the HTTP read timeout.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	// WriteTimeout is the HTTP write timeout.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// UpdateConfig holds update configuration.
type UpdateConfig struct {
	// AutoUpdateDev enables automatic updates for dev versions (default: true).
	AutoUpdateDev bool `mapstructure:"auto_update_dev"`
	// Disabled disables all update checking (default: false).
	Disabled bool `mapstructure:"disabled"`
}

// Load loads configuration from file, environment variables, and defaults.
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Config file settings
	v.SetConfigName(DefaultConfigFile)
	v.SetConfigType(DefaultConfigType)

	// Look for config in home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		v.AddConfigPath(filepath.Join(homeDir, DefaultConfigDir))
	}

	// Also look in current directory
	v.AddConfigPath(".")

	// Environment variables
	v.SetEnvPrefix("ATR")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind special environment variables
	_ = v.BindEnv("gemini.api_key", "GEMINI_API_KEY", "GOOGLE_API_KEY")
	_ = v.BindEnv("vertex.project", "GOOGLE_CLOUD_PROJECT")
	_ = v.BindEnv("vertex.location", "GOOGLE_CLOUD_LOCATION")
	_ = v.BindEnv("vertex.credentials_file", "GOOGLE_APPLICATION_CREDENTIALS")

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values.
func setDefaults(v *viper.Viper) {
	// Backend defaults
	v.SetDefault("backend", "gemini-api")
	v.SetDefault("model", "flash")

	// Vertex AI defaults
	v.SetDefault("vertex.location", "global")

	// Model name defaults
	v.SetDefault("models.flash", "gemini-3-flash-preview")
	v.SetDefault("models.pro", "gemini-3-pro-preview")

	// Agent defaults
	v.SetDefault("agent.max_iterations", 100)
	v.SetDefault("agent.timeout", "5m")
	v.SetDefault("agent.temperature", 0.3)

	// Executor defaults
	v.SetDefault("executor.command_timeout", "2m")
	v.SetDefault("executor.max_output_size", 10485760) // 10MB
	v.SetDefault("executor.environment.auto_detect", true)
	v.SetDefault("executor.environment.python_venv_path", "")
	v.SetDefault("executor.environment.conda_env_name", "")
	v.SetDefault("executor.environment.node_version", "")
	v.SetDefault("executor.environment.disable_python_env", false)
	v.SetDefault("executor.environment.disable_node_env", false)
	v.SetDefault("executor.environment.use_llm_detection", true)

	// Behavior testing defaults
	v.SetDefault("behavior.browser.executable", "auto")
	v.SetDefault("behavior.browser.cache_dir", "")    // Empty means use rod's default
	v.SetDefault("behavior.browser.headless", true)
	v.SetDefault("behavior.browser.ignore_https_errors", false)
	v.SetDefault("behavior.browser.viewport.width", 1920)
	v.SetDefault("behavior.browser.viewport.height", 1080)
	v.SetDefault("behavior.browser.page_timeout", "30s")
	v.SetDefault("behavior.browser.action_timeout", "10s")
	v.SetDefault("behavior.browser.slow_motion", "0s")

	// Capture defaults
	v.SetDefault("behavior.capture.screenshots", true)
	v.SetDefault("behavior.capture.full_page_screenshot", false)
	v.SetDefault("behavior.capture.console_logs", true)
	v.SetDefault("behavior.capture.network_har", true)
	v.SetDefault("behavior.capture.dom_snapshot", true)
	v.SetDefault("behavior.capture.max_console_entries", 100)
	v.SetDefault("behavior.capture.max_network_requests", 50)

	// Server defaults
	v.SetDefault("server.port", 9333)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "30s")

	// Update defaults
	v.SetDefault("update.auto_update_dev", true)
	v.SetDefault("update.disabled", false)
}

// Validate checks that the configuration is valid and complete.
func (c *Config) Validate() error {
	switch c.Backend {
	case "gemini-api":
		if c.Gemini.APIKey == "" {
			return fmt.Errorf("gemini API key required: set GEMINI_API_KEY environment variable or configure in ~/.atr/config.yaml")
		}
	case "vertex-ai":
		if c.Vertex.Project == "" {
			return fmt.Errorf("vertex AI project required: set GOOGLE_CLOUD_PROJECT environment variable or configure in ~/.atr/config.yaml")
		}
	default:
		return fmt.Errorf("invalid backend: %s (must be 'gemini-api' or 'vertex-ai')", c.Backend)
	}

	if c.Model != "flash" && c.Model != "pro" {
		return fmt.Errorf("invalid model tier: %s (must be 'flash' or 'pro')", c.Model)
	}

	return nil
}

// ValidateForLLM validates configuration for LLM-dependent operations
// and returns a user-friendly error message with configuration guidance.
func (c *Config) ValidateForLLM() error {
	err := c.Validate()
	if err == nil {
		return nil
	}

	guidance := `
To configure ATR, you can:
  1. Set environment variable: GEMINI_API_KEY=your-api-key
  2. Or run: atr config init
  3. Or create ~/.atr/config.yaml with:
     backend: gemini-api
     gemini:
       api_key: your-api-key

For Vertex AI, set:
  - GOOGLE_CLOUD_PROJECT environment variable
  - backend: vertex-ai

Run 'atr test' to verify your configuration.`

	return fmt.Errorf("%w\n%s", err, guidance)
}

// GetLLMConfig returns an LLM client configuration based on the current config.
func (c *Config) GetLLMConfig() llm.Config {
	var provider llm.Provider
	switch c.Backend {
	case "gemini-api":
		provider = llm.ProviderGemini
	case "vertex-ai":
		provider = llm.ProviderVertexAI
	}

	model := c.Models.Flash
	if c.Model == "pro" {
		model = c.Models.Pro
	}

	return llm.Config{
		Provider:        provider,
		Model:           model,
		APIKey:          c.Gemini.APIKey,
		Project:         c.Vertex.Project,
		Location:        c.Vertex.Location,
		CredentialsFile: c.Vertex.CredentialsFile,
		Temperature:     c.Agent.Temperature,
	}
}

// GetModelName returns the full model name based on the model tier.
func (c *Config) GetModelName() string {
	if c.Model == "pro" {
		return c.Models.Pro
	}
	return c.Models.Flash
}

// ConfigDir returns the path to the ATR configuration directory.
func ConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, DefaultConfigDir), nil
}

// EnsureConfigDir creates the configuration directory if it doesn't exist.
func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}
