package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

const (
	// githubRepo is the GitHub repository for ATR.
	githubRepo = "imyousuf/agentic-test-runner"
	// devUpdateInterval is how often dev versions auto-update (2 days).
	devUpdateInterval = 2 * 24 * time.Hour
	// timestampFile stores the last dev update time.
	timestampFileName = "dev-update-timestamp"
)

var (
	checkOnlyFlag bool
	forceFlag     bool
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install updates",
		Long: `Check for new versions of ATR and optionally install them.

By default, this command will download and install the latest version.
For dev versions, ATR auto-updates every 2 days on startup.`,
		Example: `  # Check for updates without installing
  atr update --check

  # Update to latest version
  atr update

  # Force update even if already up to date
  atr update --force`,
		RunE: runUpdate,
	}

	cmd.Flags().BoolVar(&checkOnlyFlag, "check", false, "Only check for updates, don't install")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "Force update even if already on latest version")

	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	osName, arch := detectPlatform()
	fmt.Printf("Current version: %s\n", Version)
	fmt.Printf("Platform: %s/%s\n", osName, arch)

	if isWSL() {
		fmt.Println("(Running in WSL)")
	}
	fmt.Println()

	// Determine release tag to download
	var releaseTag string
	if isDevVersion(Version) {
		releaseTag = "dev"
		fmt.Println("Dev version detected - checking for latest dev release...")
	} else {
		fmt.Println("Checking for latest stable release...")
		tag, err := getLatestReleaseTag()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		releaseTag = tag
		fmt.Printf("Latest version: %s\n", releaseTag)
	}

	if checkOnlyFlag {
		downloadURL := buildDownloadURL(releaseTag, osName, arch)
		fmt.Printf("\nDownload URL: %s\n", downloadURL)
		fmt.Println("\nRun 'atr update' to install.")
		return nil
	}

	// Check if we should skip (for dev versions without force)
	if isDevVersion(Version) && !forceFlag && !shouldAutoUpdateDev() {
		lastUpdate, _ := getLastDevUpdateTime()
		nextUpdate := lastUpdate.Add(devUpdateInterval)
		fmt.Printf("Last dev update: %s\n", lastUpdate.Format(time.RFC3339))
		fmt.Printf("Next auto-update: %s\n", nextUpdate.Format(time.RFC3339))
		fmt.Println("\nUse --force to update now.")
		return nil
	}

	// Download and install
	if err := downloadAndInstall(releaseTag, osName, arch); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	// Record update time for dev versions
	if isDevVersion(Version) {
		if err := recordDevUpdate(); err != nil {
			fmt.Printf("Warning: failed to record update time: %v\n", err)
		}
	}

	fmt.Println("\nUpdate complete! Please restart ATR to use the new version.")
	return nil
}

// detectPlatform returns the OS and architecture for the current system.
func detectPlatform() (string, string) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// WSL should use Linux binaries
	if osName == "linux" && isWSL() {
		osName = "linux"
	}

	return osName, arch
}

// isWSL detects if running in Windows Subsystem for Linux.
func isWSL() bool {
	// Check for WSL-specific file
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err == nil {
		return true
	}

	// Check /proc/version for WSL indicators
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}

	content := strings.ToLower(string(data))
	return strings.Contains(content, "microsoft") || strings.Contains(content, "wsl")
}

// isDevVersion checks if the given version is a dev version.
func isDevVersion(version string) bool {
	return version == "dev" || strings.HasPrefix(version, "dev-")
}

// buildDownloadURL constructs the GitHub release download URL for the given version and platform.
func buildDownloadURL(tag, osName, arch string) string {
	ext := "tar.gz"
	if osName == "windows" {
		ext = "zip"
	}
	assetName := fmt.Sprintf("atr-%s-%s.%s", osName, arch, ext)
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", githubRepo, tag, assetName)
}

// githubRelease represents a GitHub release response.
type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// getLatestReleaseTag fetches the latest release tag from GitHub.
func getLatestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", fmt.Sprintf("atr/%s", Version))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
}

// shouldAutoUpdateDev checks if enough time has passed since the last dev update.
func shouldAutoUpdateDev() bool {
	lastUpdate, err := getLastDevUpdateTime()
	if err != nil {
		// No timestamp = never updated = should update
		return true
	}
	return time.Since(lastUpdate) >= devUpdateInterval
}

// getLastDevUpdateTime reads the last dev update timestamp.
func getLastDevUpdateTime() (time.Time, error) {
	timestampPath, err := getTimestampPath()
	if err != nil {
		return time.Time{}, err
	}

	data, err := os.ReadFile(timestampPath)
	if err != nil {
		return time.Time{}, err
	}

	return time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
}

// recordDevUpdate saves the current time as the last dev update.
func recordDevUpdate() error {
	timestampPath, err := getTimestampPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := config.EnsureConfigDir(); err != nil {
		return err
	}

	return os.WriteFile(timestampPath, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// getTimestampPath returns the path to the timestamp file.
func getTimestampPath() (string, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, timestampFileName), nil
}

// downloadAndInstall downloads and installs the update.
func downloadAndInstall(version, osName, arch string) error {
	downloadURL := buildDownloadURL(version, osName, arch)
	fmt.Printf("Downloading from: %s\n", downloadURL)

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "atr-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Determine archive extension
	ext := "tar.gz"
	if osName == "windows" {
		ext = "zip"
	}
	archivePath := filepath.Join(tmpDir, fmt.Sprintf("atr.%s", ext))

	// Download the archive
	if err := downloadFile(downloadURL, archivePath); err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	fmt.Println("Download complete. Extracting...")

	// Extract the binary
	binaryName := "atr"
	if osName == "windows" {
		binaryName = "atr.exe"
	}
	extractedPath := filepath.Join(tmpDir, binaryName)

	if ext == "zip" {
		if err := extractZip(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("failed to extract zip: %w", err)
		}
	} else {
		if err := extractTarGz(archivePath, tmpDir, binaryName); err != nil {
			return fmt.Errorf("failed to extract tar.gz: %w", err)
		}
	}

	// Verify extracted file exists
	if _, err := os.Stat(extractedPath); err != nil {
		return fmt.Errorf("extracted binary not found: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	fmt.Printf("Installing to: %s\n", execPath)

	// Replace the binary
	if err := replaceBinary(extractedPath, execPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

// downloadFile downloads a file from the given URL.
func downloadFile(url, destPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("atr/%s", Version))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// extractTarGz extracts a specific file from a tar.gz archive.
func extractTarGz(archivePath, destDir, targetFile string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Only extract the target file
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == targetFile {
			destPath := filepath.Join(destDir, targetFile)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			return nil
		}
	}

	return fmt.Errorf("file %s not found in archive", targetFile)
}

// extractZip extracts a specific file from a zip archive.
func extractZip(archivePath, destDir, targetFile string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == targetFile {
			rc, err := f.Open()
			if err != nil {
				return err
			}

			destPath := filepath.Join(destDir, targetFile)
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, f.Mode())
			if err != nil {
				rc.Close()
				return err
			}

			_, err = io.Copy(out, rc)
			rc.Close()
			out.Close()
			if err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("file %s not found in archive", targetFile)
}

// replaceBinary replaces the current binary with the new one.
func replaceBinary(newBinary, currentBinary string) error {
	// On Windows, we need to rename the old binary first
	if runtime.GOOS == "windows" {
		oldPath := currentBinary + ".old"
		os.Remove(oldPath) // Remove any existing .old file
		if err := os.Rename(currentBinary, oldPath); err != nil {
			return fmt.Errorf("failed to rename old binary: %w", err)
		}
		if err := copyFile(newBinary, currentBinary); err != nil {
			// Try to restore old binary
			os.Rename(oldPath, currentBinary)
			return fmt.Errorf("failed to copy new binary: %w", err)
		}
		os.Remove(oldPath)
		return nil
	}

	// On Unix, we can rename over the existing file
	return copyFile(newBinary, currentBinary)
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CheckAndAutoUpdateDev checks if a dev version should be auto-updated.
// Call this on CLI startup. Returns true if an update was performed.
func CheckAndAutoUpdateDev() bool {
	if !isDevVersion(Version) {
		return false
	}

	if !shouldAutoUpdateDev() {
		return false
	}

	osName, arch := detectPlatform()
	fmt.Println("[Checking for dev version update...]")

	if err := downloadAndInstall("dev", osName, arch); err != nil {
		// Silent failure for background update
		return false
	}

	if err := recordDevUpdate(); err != nil {
		// Silent failure
	}

	fmt.Println("[Dev version auto-updated. Restart to use new version.]")
	return true
}
