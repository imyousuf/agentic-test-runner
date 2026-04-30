package config

import "testing"

// TestComputerEnvBindings verifies that ATR_COMPUTER_* env vars actually
// propagate through Viper into the unmarshalled Config.
//
// This is the contract the daemon spawn relies on:
//   atr computer start --no-gui
//     -> os.Setenv("ATR_COMPUTER_GUI_ENABLED", "false")
//     -> exec.Command("atr computer serve") (env inherited)
//     -> config.Load() in the child
//     -> cfg.Computer.GUI.Enabled == false
//
// If this drifts (Viper version change, key rename, missing BindEnv),
// CLI flags would be silently dropped at the daemon boundary.
func TestComputerEnvBindings(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		wantGUI  bool
		wantMode string
		wantSecs int
		wantPort int
	}{
		{
			name:     "defaults",
			env:      nil,
			wantGUI:  true,
			wantMode: "per-request",
			wantSecs: 3,
			wantPort: 9334,
		},
		{
			name:     "ATR_COMPUTER_GUI_ENABLED=false disables GUI",
			env:      map[string]string{"ATR_COMPUTER_GUI_ENABLED": "false"},
			wantGUI:  false,
			wantMode: "per-request",
			wantSecs: 3,
			wantPort: 9334,
		},
		{
			name:     "ATR_COMPUTER_COUNTDOWN_MODE=off",
			env:      map[string]string{"ATR_COMPUTER_COUNTDOWN_MODE": "off"},
			wantGUI:  true,
			wantMode: "off",
			wantSecs: 3,
			wantPort: 9334,
		},
		{
			name:     "ATR_COMPUTER_COUNTDOWN_SECONDS=5",
			env:      map[string]string{"ATR_COMPUTER_COUNTDOWN_SECONDS": "5"},
			wantGUI:  true,
			wantMode: "per-request",
			wantSecs: 5,
			wantPort: 9334,
		},
		{
			name:     "ATR_COMPUTER_PORT=12345",
			env:      map[string]string{"ATR_COMPUTER_PORT": "12345"},
			wantGUI:  true,
			wantMode: "per-request",
			wantSecs: 3,
			wantPort: 12345,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv handles cleanup across subtests.
			t.Setenv("HOME", t.TempDir()) // isolate from any local config file
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.Computer.GUI.Enabled; got != tc.wantGUI {
				t.Errorf("Computer.GUI.Enabled = %v, want %v", got, tc.wantGUI)
			}
			if got := cfg.Computer.Countdown.Mode; got != tc.wantMode {
				t.Errorf("Computer.Countdown.Mode = %q, want %q", got, tc.wantMode)
			}
			if got := cfg.Computer.Countdown.Seconds; got != tc.wantSecs {
				t.Errorf("Computer.Countdown.Seconds = %d, want %d", got, tc.wantSecs)
			}
			if got := cfg.Computer.Port; got != tc.wantPort {
				t.Errorf("Computer.Port = %d, want %d", got, tc.wantPort)
			}
		})
	}
}
