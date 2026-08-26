package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// browserHudWorkingDir overrides the directory the HUD agent's shell, read
// and search tools operate in.
var browserHudWorkingDir string

func newBrowserHudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hud",
		Short: "Control the in-page agent panel",
		Long: `Show or hide the ATR agent panel inside the browser window.

The panel is a floating chat box injected into every page. Type a request and
the agent carries it out on the page in front of you: it can drive the browser,
run shell commands, read files and search code.

Passwords are handled without ever being shown to the agent. Ask it to fill a
credential and it runs your password manager itself, typing the output straight
into the field:

  "fill the password field by running: pass show github/password"

Name entries instead of commands by adding them to ~/.atr/config.yaml:

  secrets:
    refs:
      github/password: pass show github/password

then just ask for "the github/password secret".

The panel only makes sense with a visible browser, so start the daemon headed
(the CLI default).

Examples:
  atr browser hud on
  atr browser hud status
  atr browser hud off`,
	}

	cmd.AddCommand(newBrowserHudOnCmd())
	cmd.AddCommand(newBrowserHudOffCmd())
	cmd.AddCommand(newBrowserHudStatusCmd())
	return cmd
}

func newBrowserHudOnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "on",
		Short: "Show the agent panel in the browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/hud/enable", hudEnableBody())
		},
	}
	cmd.Flags().StringVar(&browserHudWorkingDir, "working-dir", "",
		"Directory the agent's shell, read and search tools operate in (default: current directory)")
	return cmd
}

func newBrowserHudOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Remove the agent panel from the browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiPost("/hud/disable", map[string]interface{}{})
		},
	}
}

func newBrowserHudStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the agent panel is installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGet("/hud/status")
		},
	}
}

// hudEnableBody builds the enable payload, defaulting the working directory
// to wherever the user ran the command. The daemon's own working directory is
// wherever it happened to be started, which is rarely what the user means.
func hudEnableBody() map[string]interface{} {
	body := map[string]interface{}{}

	dir := browserHudWorkingDir
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}
	if dir != "" {
		body["working_dir"] = dir
	}
	return body
}

// enableHudAfterStart turns the panel on for a daemon that has just started.
//
// It goes through apiRequestRaw rather than apiPost so that nothing is printed
// on success: `atr browser start --hud --json` must emit exactly one JSON
// document, the one describing the daemon.
//
// Failures are warnings, not errors. The browser is running either way, and
// the user can retry with `atr browser hud on`.
func enableHudAfterStart() {
	if browserHeadless {
		fmt.Fprintln(os.Stderr, "Warning: --hud has no effect with --headless; the panel needs a visible window")
		return
	}
	if _, err := apiRequestRaw("POST", "/hud/enable", hudEnableBody()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not show the agent panel: %v\n", err)
	}
}
