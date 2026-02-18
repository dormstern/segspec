package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dormorgenstern/segspec/internal/ai"
	"github.com/dormorgenstern/segspec/internal/parser"
	"github.com/dormorgenstern/segspec/internal/renderer"
	"github.com/dormorgenstern/segspec/internal/walker"
)

var aiEnabled bool

var analyzeCmd = &cobra.Command{
	Use:   "analyze <path>",
	Short: "Analyze application configs and generate network policies",
	Long: `Analyze scans a directory for application configuration files,
extracts network dependencies, and generates Kubernetes NetworkPolicy YAML.

Supported file types:
  - Spring: application.yml, application.properties
  - Docker: docker-compose.yml
  - Kubernetes: Deployment, Service, ConfigMap manifests
  - Environment: .env files
  - Build: pom.xml, build.gradle (dependency inference)

Use --ai to enable LLM-powered analysis for deeper dependency discovery.`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().BoolVar(&aiEnabled, "ai", false, "Enable LLM-powered analysis (requires ANTHROPIC_API_KEY)")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	registry := parser.DefaultRegistry()

	ds, err := walker.Walk(path, registry)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	if aiEnabled {
		aiDeps, aiErr := ai.Analyze(path, ds.Dependencies())
		if aiErr != nil {
			// If API key is missing, warn and continue with rule-based results.
			fmt.Fprintf(os.Stderr, "Warning: AI analysis skipped: %v\n", aiErr)
		} else {
			for _, dep := range aiDeps {
				ds.Add(dep)
			}
		}
	}

	if ds.Len() == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No network dependencies found.")
		return nil
	}

	out := cmd.OutOrStdout()
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	switch outputFormat {
	case "summary":
		fmt.Fprint(out, renderer.Summary(ds))
	case "netpol":
		fmt.Fprint(out, renderer.NetworkPolicy(ds))
	case "all":
		fmt.Fprint(out, renderer.Summary(ds))
		fmt.Fprintln(out, "---")
		fmt.Fprint(out, renderer.NetworkPolicy(ds))
	default:
		return fmt.Errorf("unknown format: %s (valid: summary, netpol, all)", outputFormat)
	}

	return nil
}
