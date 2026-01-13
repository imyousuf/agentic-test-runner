package llm

import (
	"os/exec"
	"strings"

	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// CLIInfo contains information about a detected CLI tool.
type CLIInfo struct {
	// Provider is the CLI provider type.
	Provider llm.Provider
	// Path is the full path to the executable.
	Path string
	// Version is the version string (if available).
	Version string
	// Available indicates if the CLI is available in PATH.
	Available bool
}

// DetectAvailableCLIs checks which CLI tools are available in the system PATH.
func DetectAvailableCLIs() []CLIInfo {
	var clis []CLIInfo

	for _, provider := range llm.CLIPriority {
		info := detectCLI(provider)
		if info.Available {
			clis = append(clis, info)
		}
	}

	return clis
}

// GetPreferredCLI returns the preferred available CLI based on priority order.
func GetPreferredCLI() (llm.Provider, bool) {
	for _, provider := range llm.CLIPriority {
		info := detectCLI(provider)
		if info.Available {
			return provider, true
		}
	}
	return "", false
}

// IsCLIAvailable checks if a specific CLI is available.
func IsCLIAvailable(provider llm.Provider) bool {
	info := detectCLI(provider)
	return info.Available
}

// detectCLI checks if a CLI tool is available and gathers info about it.
func detectCLI(provider llm.Provider) CLIInfo {
	executable := provider.CLIExecutable()
	if executable == "" {
		return CLIInfo{Provider: provider, Available: false}
	}

	// Find executable using common paths, then fall back to PATH
	var path string
	switch provider {
	case llm.ProviderClaudeCLI:
		path = findClaudeCLI()
	case llm.ProviderGeminiCLI:
		path = findGeminiCLI()
	default:
		// For unknown providers, just check PATH
		var err error
		path, err = exec.LookPath(executable)
		if err != nil {
			return CLIInfo{Provider: provider, Available: false}
		}
	}

	if path == "" {
		return CLIInfo{Provider: provider, Available: false}
	}

	info := CLIInfo{
		Provider:  provider,
		Path:      path,
		Available: true,
	}

	// Try to get version
	info.Version = getCLIVersion(provider, path)

	return info
}

// getCLIVersion attempts to get the version of a CLI tool.
func getCLIVersion(provider llm.Provider, path string) string {
	var args []string

	switch provider {
	case llm.ProviderClaudeCLI:
		args = []string{"--version"}
	case llm.ProviderGeminiCLI:
		args = []string{"--version"}
	default:
		return ""
	}

	cmd := exec.Command(path, args...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Parse version from output
	version := strings.TrimSpace(string(output))

	// Extract just the version number if possible
	switch provider {
	case llm.ProviderClaudeCLI:
		// Claude CLI outputs like "claude v2.1.3"
		parts := strings.Fields(version)
		for _, part := range parts {
			if strings.HasPrefix(part, "v") || strings.Contains(part, ".") {
				return part
			}
		}
	case llm.ProviderGeminiCLI:
		// Gemini CLI outputs version in various formats
		parts := strings.Fields(version)
		for _, part := range parts {
			if strings.Contains(part, ".") {
				return part
			}
		}
	}

	// Return first line if version parsing fails
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		return lines[0]
	}

	return version
}
