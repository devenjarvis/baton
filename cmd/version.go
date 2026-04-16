package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version metadata. Populated at build time via -ldflags by GoReleaser; kept
// as var (not const) so the linker can override them. Default values are the
// ones produced by a bare `go build`.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// versionString returns the canonical "baton <version> (<commit>, <date>)" format
// used by both the `--version` flag and the `version` subcommand.
func versionString() string {
	return fmt.Sprintf("baton %s (%s, %s)", Version, Commit, Date)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionString())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
