// Package cli implements the command-line interface for ATR.
package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInstallCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-completion",
		Short: "Install shell completion for bash or zsh",
		Long: `Install shell completion script for ATR.

Automatically detects your shell (bash or zsh) and installs the completion script.

If running with sudo/root, installs system-wide:
  - Bash: /etc/bash_completion.d/atr
  - Zsh: /usr/local/share/zsh/site-functions/_atr

Otherwise, installs for current user:
  - Bash: ~/.bash_completion.d/atr (sources from ~/.bashrc)
  - Zsh: ~/.zsh/completions/_atr (add to fpath in ~/.zshrc)
`,
		RunE: runInstallCompletion,
	}

	return cmd
}

func runInstallCompletion(cmd *cobra.Command, args []string) error {
	shell := detectShell()
	if shell == "" {
		return fmt.Errorf("could not detect shell (SHELL env not set or unsupported shell)")
	}

	isRoot := isRunningAsRoot()

	var completionPath string
	var completionContent bytes.Buffer
	var postInstallMsg string

	switch shell {
	case "bash":
		if err := cmd.Root().GenBashCompletion(&completionContent); err != nil {
			return fmt.Errorf("failed to generate bash completion: %w", err)
		}
		if isRoot {
			completionPath = "/etc/bash_completion.d/atr"
			postInstallMsg = "Completion will be available in new shells."
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			completionPath = filepath.Join(homeDir, ".bash_completion.d", "atr")
			postInstallMsg = `Add to your ~/.bashrc if not already present:
  for f in ~/.bash_completion.d/*; do source "$f"; done

Then reload: source ~/.bashrc`
		}

	case "zsh":
		if err := cmd.Root().GenZshCompletion(&completionContent); err != nil {
			return fmt.Errorf("failed to generate zsh completion: %w", err)
		}
		if isRoot {
			completionPath = "/usr/local/share/zsh/site-functions/_atr"
			postInstallMsg = "Completion will be available in new shells."
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			completionPath = filepath.Join(homeDir, ".zsh", "completions", "_atr")
			postInstallMsg = `Add to your ~/.zshrc if not already present:
  fpath=(~/.zsh/completions $fpath)
  autoload -U compinit && compinit

Then reload: source ~/.zshrc`
		}

	default:
		return fmt.Errorf("unsupported shell: %s (only bash and zsh are supported)", shell)
	}

	// Create parent directory if needed
	parentDir := filepath.Dir(completionPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
	}

	// Write completion file
	if err := os.WriteFile(completionPath, completionContent.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write completion file: %w", err)
	}

	fmt.Printf("Installed %s completion to: %s\n\n%s\n", shell, completionPath, postInstallMsg)
	return nil
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}

	base := filepath.Base(shell)
	switch {
	case strings.Contains(base, "bash"):
		return "bash"
	case strings.Contains(base, "zsh"):
		return "zsh"
	default:
		return base
	}
}

func isRunningAsRoot() bool {
	// Check effective UID
	if os.Geteuid() == 0 {
		return true
	}

	// Also check if running via sudo
	if os.Getenv("SUDO_USER") != "" {
		return true
	}

	// Check current user
	currentUser, err := user.Current()
	if err != nil {
		return false
	}

	return currentUser.Uid == "0"
}
