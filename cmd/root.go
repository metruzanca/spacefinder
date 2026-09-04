package cmd

import (
	"os"

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

Without a path, a picker lets you choose a filesystem or directory to scan:
home, root, the current directory, and any other detected drives or
partitions.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	Version:      Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := ""
		if len(args) == 1 {
			root = args[0]
		}
		if root == "" {
			logging.Debugf("no path given; showing filesystem picker")
		} else {
			logging.Debugf("tui root path: %s", root)
		}
		return tui.Run(root)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
