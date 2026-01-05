package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version information (set by ldflags during build)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("atr version %s\n", Version)
			fmt.Printf("  commit: %s\n", Commit)
			fmt.Printf("  built: %s\n", BuildDate)
			fmt.Printf("  go: %s\n", runtime.Version())
			fmt.Printf("  os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}
