package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the segspec release version. Set via -ldflags at release time;
// the in-tree default is the current development version so `segspec version`
// always reports something meaningful when run from a `go install` build.
var Version = "0.6.0-dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print segspec version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("segspec %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
