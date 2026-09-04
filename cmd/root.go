package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/metruzanca/spacefinder/internal/logging"
	"github.com/metruzanca/spacefinder/internal/tui"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "spacefinder [path]",
	Short: "Visualize disk usage as a treemap in your terminal",
	Long: `spacefinder scans a directory tree and renders it as a squarified
treemap spanning the whole screen, the way spacesniffer and filelight do.
Navigate with the arrow or vim keys, drill into directories with Enter,
double-click a file to open it in its default app, and delete junk (after
typing its name to confirm) with Delete.

Without a path, the scan starts at $HOME, or at / when run as root.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	Version:      Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := defaultRoot()
		if len(args) == 1 {
			root = args[0]
		}
		logging.Debugf("tui root path: %s", root)
		return tui.Run(root)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// defaultRoot picks the scan root: / when spacefinder is run as root, $HOME
// otherwise.
func defaultRoot() string {
	if os.Geteuid() == 0 {
		return string(os.PathSeparator)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		logging.Errorf("resolving $HOME: %v", err)
		return filepath.VolumeName(home) + string(os.PathSeparator)
	}
	return home
}
