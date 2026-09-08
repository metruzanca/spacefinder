package cmd

import (
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/metruzanca/spacefinder/internal/logging"
	"github.com/metruzanca/spacefinder/internal/tui"
)

// Version is the release version, injected at build time via ldflags for
// release artifacts. When built with `go install pkg@version` no ldflags apply,
// so the fallback in init() fills it from the module version embedded by the
// go tool.
var Version = "dev"

func init() {
	if Version == "dev" {
		if v := moduleVersion(); v != "" {
			Version = v
		}
	}
	rootCmd.Version = Version
}

// moduleVersion returns the main-module version recorded in the binary's build
// info, e.g. "v0.7.0" for `go install spacefinder@v0.7.0`, or "" when it is not
// available (a local build compiled without a module version).
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return ""
}

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
	RunE: func(cmd *cobra.Command, args []string) error {
		logging.Debugf("command invoked: %s", cmd.CommandPath())
		root := ""
		if len(args) == 1 {
			root = args[0]
		}
		if root == "" {
			logging.Debugf("no path given; showing filesystem picker")
		} else {
			logging.Debugf("tui root path: %s", root)
		}
		err := tui.Run(root)
		if err != nil {
			logging.Errorf("tui exited with error: %v", err)
			return err
		}
		logging.Debugf("tui exited cleanly")
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
