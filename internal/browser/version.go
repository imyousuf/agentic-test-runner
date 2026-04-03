package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Chrome for Testing API endpoint
	chromeForTestingAPI = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"
	// Cache file name for browser version
	versionCacheFile = "browser-version.json"
	// Cache validity duration (24 hours)
	versionCacheDuration = 24 * time.Hour
)

// VersionCache stores the cached browser version info.
type VersionCache struct {
	Version  string    `json:"version"`
	Revision string    `json:"revision"`
	Channel  string    `json:"channel"`
	Platform string    `json:"platform"`
	URL      string    `json:"url"`
	CachedAt time.Time `json:"cached_at"`
}

// ChromeForTestingResponse represents the API response structure.
type ChromeForTestingResponse struct {
	Timestamp string `json:"timestamp"`
	Channels  struct {
		Stable ChannelInfo `json:"Stable"`
		Beta   ChannelInfo `json:"Beta"`
		Dev    ChannelInfo `json:"Dev"`
		Canary ChannelInfo `json:"Canary"`
	} `json:"channels"`
}

// ChannelInfo contains version info for a release channel.
type ChannelInfo struct {
	Channel   string `json:"channel"`
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Downloads struct {
		Chrome []PlatformDownload `json:"chrome"`
	} `json:"downloads"`
}

// PlatformDownload contains download info for a platform.
type PlatformDownload struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

// ResolveVersion resolves the browser version based on the config.
// Returns the download URL and version string.
// Supported values: "latest", "stable", "beta", "dev", "canary", or a specific version.
func ResolveVersion(configVersion string, atrDir string) (*VersionCache, error) {
	if configVersion == "" {
		return nil, nil // Use rod's default
	}

	// Normalize version string
	version := strings.ToLower(strings.TrimSpace(configVersion))

	// Check if it's a channel name
	channel := ""
	switch version {
	case "latest", "stable":
		channel = "Stable"
	case "beta":
		channel = "Beta"
	case "dev":
		channel = "Dev"
	case "canary":
		channel = "Canary"
	default:
		// Specific version requested - not supported via Chrome for Testing API lookup
		// User should provide executable path for specific versions
		return nil, fmt.Errorf("specific version '%s' not supported; use 'latest', 'stable', 'beta', 'dev', 'canary', or provide executable path", configVersion)
	}

	// Try to load from cache first
	cacheFile := filepath.Join(atrDir, versionCacheFile)
	cached, err := loadVersionCache(cacheFile)
	if err == nil && cached != nil {
		// Check if cache is still valid and matches requested channel
		if cached.Channel == channel && time.Since(cached.CachedAt) < versionCacheDuration {
			return cached, nil
		}
	}

	// Fetch from API
	versionInfo, err := fetchLatestVersion(channel)
	if err != nil {
		// If we have a stale cache, use it as fallback
		if cached != nil && cached.Channel == channel {
			return cached, nil
		}
		return nil, fmt.Errorf("failed to fetch browser version: %w", err)
	}

	// Save to cache
	if err := saveVersionCache(cacheFile, versionInfo); err != nil {
		// Non-fatal, just log it would happen here
		fmt.Fprintf(os.Stderr, "Warning: failed to cache browser version: %v\n", err)
	}

	return versionInfo, nil
}

// fetchLatestVersion fetches the latest version info from Chrome for Testing API.
func fetchLatestVersion(channel string) (*VersionCache, error) {
	resp, err := http.Get(chromeForTestingAPI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Chrome for Testing API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Chrome for Testing API returned status %d", resp.StatusCode)
	}

	var apiResp ChromeForTestingResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	// Get channel info
	var channelInfo ChannelInfo
	switch channel {
	case "Stable":
		channelInfo = apiResp.Channels.Stable
	case "Beta":
		channelInfo = apiResp.Channels.Beta
	case "Dev":
		channelInfo = apiResp.Channels.Dev
	case "Canary":
		channelInfo = apiResp.Channels.Canary
	default:
		return nil, fmt.Errorf("unknown channel: %s", channel)
	}

	// Determine platform
	platform := getPlatform()
	if platform == "" {
		return nil, fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Find download URL for platform
	var downloadURL string
	for _, dl := range channelInfo.Downloads.Chrome {
		if dl.Platform == platform {
			downloadURL = dl.URL
			break
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no download available for platform %s", platform)
	}

	return &VersionCache{
		Version:  channelInfo.Version,
		Revision: channelInfo.Revision,
		Channel:  channel,
		Platform: platform,
		URL:      downloadURL,
		CachedAt: time.Now(),
	}, nil
}

// getPlatform returns the Chrome for Testing platform string.
func getPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-arm64"
		}
		return "mac-x64"
	case "linux":
		return "linux64"
	case "windows":
		if runtime.GOARCH == "386" {
			return "win32"
		}
		return "win64"
	default:
		return ""
	}
}

// loadVersionCache loads the cached version info.
func loadVersionCache(cacheFile string) (*VersionCache, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, err
	}

	var cache VersionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// saveVersionCache saves version info to cache file.
func saveVersionCache(cacheFile string, info *VersionCache) error {
	// Ensure directory exists
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFile, data, 0644)
}

// GetATRDir returns the ATR configuration directory path.
func GetATRDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".atr"), nil
}

// expandPath expands ~ to home directory and resolves relative paths.
// Returns an error if path expansion fails.
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to expand ~: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to resolve relative path: %w", err)
		}
		path = filepath.Join(cwd, path)
	}
	return path, nil
}
