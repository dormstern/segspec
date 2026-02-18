package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	outputFormat string
	outputFile   string
)

var rootCmd = &cobra.Command{
	Use:   "segspec",
	Short: "Generate Kubernetes NetworkPolicy from application configs",
	Long: `segspec analyzes application configuration files and generates
Kubernetes NetworkPolicy YAML for microsegmentation.

Point it at your app directory. It reads configs, infers network
dependencies, and outputs ready-to-apply policies.

  segspec analyze ./my-app/
  segspec analyze ./my-app/ --format netpol
  segspec analyze ./my-app/ --ai`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "format", "f", "summary", "Output format: summary, netpol, per-service, all")
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "", "Write output to file (default: stdout)")
}
