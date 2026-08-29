// Package config handles configuration loading and validation for ATR.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/imyousuf/agentic-test-runner/internal/secret"
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

	// CLI contains CLI backend configuration.
	CLI CLIConfig `mapstructure:"cli"`

	// Model specifies which model tier to use (flash or pro).
	Model string `mapstructure:"model"`

	// Models contains model name mappings.
	Models ModelsConfig `mapstructure:"models"`

	// Agent contains agent behavior configuration.
	Agent AgentConfig `mapstructure:"agent"`

	// Executor contains command execution configuration.
	Executor ExecutorConfig `mapstructure:"executor"`

	// Secrets contains named secret references for the browser HUD agent.
	Secrets secret.Config `mapstructure:"secrets"`

	// Behavior contains browser behavior testing configuration.
	Behavior BehaviorConfig `mapstructure:"behavior"`

	// Server contains browser server configuration.
	Server ServerConfig `mapstructure:"server"`

	// Computer contains desktop computer-use configuration.
	Computer ComputerConfig `mapstructure:"computer"`

	// Update contains update configuration.
	Update UpdateConfig `mapstructure:"update"`

	// History contains execution-history configuration.
	History HistoryConfig `mapstructure:"history"`

	// Telemetry contains OpenTelemetry configuration.
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
}

// HistoryConfig holds the local execution history settings.
type HistoryConfig struct {
	// Enabled records every behaviour run in a local database. On by
	// default: it is cheap, private, and inside the same trust boundary as
	// the browser the user just drove.
	Enabled bool `mapstructure:"enabled"`
	// Path is the database file. Empty means ~/.atr/history.db.
	Path string `mapstructure:"path"`
	// KeepDays bounds how long a run is kept. A machine running a suite in a
	// loop would otherwise grow a row per attempt for ever.
	KeepDays int `mapstructure:"keep_days"`
}

// TelemetryConfig holds the OpenTelemetry settings.
//
// There is no endpoint here on purpose: OTEL_EXPORTER_OTLP_ENDPOINT is the
// standard variable, so a laptop with no collector emits nothing and produces
// no connection errors, and a CI job opts in with one line and no ATR-specific
// knowledge.
type TelemetryConfig struct {
	// Enabled allows export when an endpoint is configured. On by default,
	// and inert without one.
	Enabled bool `mapstructure:"enabled"`
	// ServiceName names ATR in the collector.
	ServiceName string `mapstructure:"service_name"`
	// ShutdownTimeout bounds the flush on exit. A replay takes nine seconds
	// and exits; without an explicit flush a batch processor's schedule never
	// fires and CI exports nothing.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
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

// CLIConfig holds CLI backend configuration.
type CLIConfig struct {
	// AutoDetect enables automatic CLI detection (default: true).
	AutoDetect bool `mapstructure:"auto_detect"`
	// Timeout is the maximum time for CLI execution.
	Timeout time.Duration `mapstructure:"timeout"`
}

// ModelsConfig holds model name mappings.
type ModelsConfig struct {
	// Flash is the model name for the flash tier.
	Flash string `mapstructure:"flash"`
	// Pro is the model name for the pro tier.
	Pro string `mapstructure:"pro"`
	// Sonnet is the model name for the sonnet tier (vertex-claude backend).
	Sonnet string `mapstructure:"sonnet"`
	// Opus is the model name for the opus tier (vertex-claude backend).
	Opus string `mapstructure:"opus"`
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
	// Version specifies the browser version to download ("latest", "stable", or specific version like "132.0.6834.83").
	// Only used when Executable is "auto". Default is empty (uses rod's bundled version).
	Version string `mapstructure:"version"`
	// CacheDir is the directory for rod's browser cache (deprecated, use DataDir).
	CacheDir string `mapstructure:"cache_dir"`
	// DataDir is the directory for browser data (cookies, localStorage, etc.).
	// Supports ~ for home directory and relative paths.
	DataDir string `mapstructure:"data_dir"`
	// PersistSession keeps cookies/session data after browser closes.
	// Uses DataDir if set, otherwise defaults to ~/.atr/browser-data.
	PersistSession bool `mapstructure:"persist_session"`
	// Headless runs browser in headless mode.
	Headless bool `mapstructure:"headless"`
	// NoSandbox disables Chrome sandbox (needed on Ubuntu 23.10+ with AppArmor restrictions).
	NoSandbox bool `mapstructure:"no_sandbox"`
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

// ComputerConfig holds desktop computer-use configuration.
type ComputerConfig struct {
	// Enabled toggles whether the computer feature is exposed via CLI/MCP.
	Enabled bool `mapstructure:"enabled"`
	// Port is the daemon HTTP port (default 9334).
	Port int `mapstructure:"port"`
	// Countdown holds safety-gate configuration.
	Countdown ComputerCountdownConfig `mapstructure:"countdown"`
	// GUI holds optional overlay configuration.
	GUI ComputerGUIConfig `mapstructure:"gui"`
	// Display selects the default monitor for screenshots (0-indexed).
	Display int `mapstructure:"display"`
}

// ComputerCountdownConfig holds safety-gate configuration.
type ComputerCountdownConfig struct {
	// Mode selects when the gate prompts: per-request, per-app, or off.
	Mode string `mapstructure:"mode"`
	// Seconds is the countdown duration before each gated action.
	Seconds int `mapstructure:"seconds"`
}

// ComputerGUIConfig holds optional GUI overlay configuration.
type ComputerGUIConfig struct {
	// Enabled toggles the webview countdown overlay.
	Enabled bool `mapstructure:"enabled"`
}

// Load loads configuration from file, environment variables, and defaults.
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Check if a specific config file was set via CLI flag (stored in global viper)
	globalViper := viper.GetViper()
	if configFile := globalViper.GetString("config_file"); configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		// Config file settings for default paths
		v.SetConfigName(DefaultConfigFile)
		v.SetConfigType(DefaultConfigType)

		// Look for config in home directory
		homeDir, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(filepath.Join(homeDir, DefaultConfigDir))
		}

		// Also look in .atr directory in current directory (project-level config)
		v.AddConfigPath(".atr")
	}

	// Environment variables
	v.SetEnvPrefix("ATR")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind special environment variables
	_ = v.BindEnv("gemini.api_key", "GEMINI_API_KEY", "GOOGLE_API_KEY")
	_ = v.BindEnv("vertex.project", "GOOGLE_CLOUD_PROJECT")
	_ = v.BindEnv("vertex.location", "GOOGLE_CLOUD_LOCATION")
	_ = v.BindEnv("vertex.credentials_file", "GOOGLE_APPLICATION_CREDENTIALS")

	// Computer feature env bindings (also reachable via AutomaticEnv, but
	// explicit binding ensures struct unmarshal sees the override even when
	// no config file is present).
	_ = v.BindEnv("computer.gui.enabled", "ATR_COMPUTER_GUI_ENABLED")
	_ = v.BindEnv("computer.countdown.mode", "ATR_COMPUTER_COUNTDOWN_MODE")
	_ = v.BindEnv("computer.countdown.seconds", "ATR_COMPUTER_COUNTDOWN_SECONDS")
	_ = v.BindEnv("computer.port", "ATR_COMPUTER_PORT")

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Merge CLI flag values from global viper (flags are bound there in root.go)
	// CLI flags have highest precedence over config file and env vars
	for _, key := range []string{"backend", "model", "gemini.api_key", "vertex.project", "vertex.location"} {
		if globalViper.IsSet(key) {
			v.Set(key, globalViper.Get(key))
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

	// CLI defaults
	v.SetDefault("cli.auto_detect", true)
	v.SetDefault("cli.timeout", "5m")

	// Model name defaults
	v.SetDefault("models.flash", "gemini-3.7-flash")
	v.SetDefault("models.pro", "gemini-3.1-pro-preview")
	v.SetDefault("models.sonnet", "claude-sonnet-5")
	v.SetDefault("models.opus", "claude-opus-5")

	// Agent defaults
	// Both sinks may be disabled, including at the same time. Requiring at
	// least one would mean writing to disk after a user said not to, which is
	// a small betrayal a developer tool does not recover from — and it buys
	// nothing, since a local tool cannot phone home regardless.
	v.SetDefault("history.enabled", true)
	v.SetDefault("history.keep_days", 90)
	v.SetDefault("telemetry.enabled", true)
	v.SetDefault("telemetry.service_name", "atr")
	v.SetDefault("telemetry.shutdown_timeout", "5s")

	v.SetDefault("agent.max_iterations", 100)
	v.SetDefault("agent.timeout", "5m")
	v.SetDefault("agent.temperature", 0.3)

	// Executor defaults
	// Secrets have no default refs: an entry only exists once the user has
	// pointed it at a command in their own password manager.
	v.SetDefault("secrets.timeout", "60s")
	v.SetDefault("secrets.keep_trailing_newline", false)

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
	v.SetDefault("behavior.browser.version", "latest") // Download latest Chrome for Testing
	v.SetDefault("behavior.browser.cache_dir", "")     // Empty means use rod's default (deprecated, use data_dir)
	v.SetDefault("behavior.browser.data_dir", "")      // Empty = use cache_dir or default when persist_session is true
	v.SetDefault("behavior.browser.persist_session", false)
	v.SetDefault("behavior.browser.headless", true)
	v.SetDefault("behavior.browser.no_sandbox", false)
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

	// Computer (desktop computer-use) defaults
	v.SetDefault("computer.enabled", true)
	v.SetDefault("computer.port", 9334)
	v.SetDefault("computer.countdown.mode", "per-request")
	v.SetDefault("computer.countdown.seconds", 3)
	v.SetDefault("computer.gui.enabled", true)
	v.SetDefault("computer.display", 0)
}

// Validate checks that the configuration is valid and complete.
func (c *Config) Validate() error {
	switch c.Backend {
	case "gemini-api":
		if c.Gemini.APIKey == "" {
			return fmt.Errorf("gemini API key required: set GEMINI_API_KEY environment variable or configure in ~/.atr/config.yaml")
		}
	case "vertex-ai", "vertex-claude":
		if c.Vertex.Project == "" {
			return fmt.Errorf("vertex AI project required: set GOOGLE_CLOUD_PROJECT environment variable or configure in ~/.atr/config.yaml")
		}
	case "claude-cli", "gemini-cli":
		// CLI backends don't require API keys - they use the CLI's authentication
		// Validation of CLI availability is done at runtime
	default:
		return fmt.Errorf("invalid backend: %s (must be 'gemini-api', 'vertex-ai', 'vertex-claude', 'claude-cli', or 'gemini-cli')", c.Backend)
	}

	// Validate model based on backend type
	if c.IsCLIBackend() {
		// For CLI backends, validate model aliases
		if c.Backend == "claude-cli" && c.Model != "" {
			validModels := map[string]bool{
				"opus": true, "sonnet": true, "haiku": true,
				"claude-opus": true, "claude-sonnet": true, "claude-haiku": true,
				"claude-opus-4": true, "claude-sonnet-4": true, "claude-haiku-4": true,
			}
			// Also allow full model names like claude-opus-4-20250514
			if !validModels[strings.ToLower(c.Model)] && !strings.HasPrefix(c.Model, "claude-") {
				return fmt.Errorf("invalid claude-cli model: %s (use 'opus', 'sonnet', or 'haiku')", c.Model)
			}
		}
		// gemini-cli model validation can be added when needed
	} else if c.Backend == "vertex-claude" {
		// The Claude tiers are sonnet and opus. flash/pro are accepted as
		// aliases so that switching backend does not also force a model
		// change: the stock config ships model "flash".
		if !isClaudeTier(c.Model) {
			return fmt.Errorf("invalid model tier: %s (must be 'sonnet' or 'opus')", c.Model)
		}
	} else {
		// For API backends, validate model tier
		if c.Model != "flash" && c.Model != "pro" {
			return fmt.Errorf("invalid model tier: %s (must be 'flash' or 'pro')", c.Model)
		}
	}

	return nil
}

// IsCLIBackend returns true if the configured backend is CLI-based.
func (c *Config) IsCLIBackend() bool {
	return c.Backend == "claude-cli" || c.Backend == "gemini-cli"
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

For Claude on Vertex AI (prompt caching, Application Default Credentials):
  - Run: gcloud auth application-default login
  - GOOGLE_CLOUD_PROJECT environment variable
  - backend: vertex-claude
  - model: sonnet (or opus)

For CLI backends (no API key needed):
  - Install claude CLI and set: backend: claude-cli
  - Or install gemini CLI and set: backend: gemini-cli

Run 'atr test' to verify your configuration.`

	return fmt.Errorf("%w\n%s", err, guidance)
}

// GetLLMConfig returns an LLM client configuration based on the current config.
// For CLI backends, this returns a config with the CLI provider set.
func (c *Config) GetLLMConfig() llm.Config {
	var provider llm.Provider
	switch c.Backend {
	case "gemini-api":
		provider = llm.ProviderGemini
	case "vertex-ai":
		provider = llm.ProviderVertexAI
	case "vertex-claude":
		provider = llm.ProviderVertexClaude
	case "claude-cli":
		provider = llm.ProviderClaudeCLI
	case "gemini-cli":
		provider = llm.ProviderGeminiCLI
	}

	// Determine model based on backend type
	var model string
	if c.IsCLIBackend() {
		// For CLI backends, pass the model directly (opus, sonnet, haiku)
		// If not specified, CLI will use its default
		model = c.Model
	} else if c.Backend == "vertex-claude" {
		model = c.claudeModelName()
	} else {
		// For API backends, use flash/pro tier mapping
		model = c.Models.Flash
		if c.Model == "pro" {
			model = c.Models.Pro
		}
	}

	return llm.Config{
		Provider:        provider,
		Model:           model,
		Timeout:         c.GetCLITimeout(),
		APIKey:          c.Gemini.APIKey,
		Project:         c.Vertex.Project,
		Location:        c.Vertex.Location,
		CredentialsFile: c.Vertex.CredentialsFile,
		Temperature:     c.Agent.Temperature,
	}
}

// GetCLITimeout returns the CLI execution timeout.
func (c *Config) GetCLITimeout() time.Duration {
	if c.CLI.Timeout > 0 {
		return c.CLI.Timeout
	}
	return 5 * time.Minute
}

// HistoryPath is where the execution history lives.
func (c *Config) HistoryPath() string {
	if c.History.Path != "" {
		return c.History.Path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "history.db"
	}
	return filepath.Join(home, DefaultConfigDir, "history.db")
}

// HistoryKeep is how long a run is kept, zero meaning for ever.
func (c *Config) HistoryKeep() time.Duration {
	if c.History.KeepDays <= 0 {
		return 0
	}
	return time.Duration(c.History.KeepDays) * 24 * time.Hour
}

// GetModelName returns the full model name based on the model tier.
func (c *Config) GetModelName() string {
	if c.Backend == "vertex-claude" {
		return c.claudeModelName()
	}
	if c.Model == "pro" {
		return c.Models.Pro
	}
	return c.Models.Flash
}

// isClaudeTier reports whether tier names a Claude model tier. "flash" and
// "pro" are accepted as aliases of sonnet and opus.
func isClaudeTier(tier string) bool {
	switch strings.ToLower(tier) {
	case "sonnet", "opus", "flash", "pro", "":
		return true
	}
	return false
}

// claudeModelName resolves the configured tier to a Vertex publisher model id.
func (c *Config) claudeModelName() string {
	switch strings.ToLower(c.Model) {
	case "opus", "pro":
		return c.Models.Opus
	default:
		return c.Models.Sonnet
	}
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
