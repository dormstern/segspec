package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/ai"
	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/renderer"
	"github.com/dormstern/segspec/internal/tui"
	"github.com/dormstern/segspec/internal/walker"
)

var aiProvider string
var interactive bool
var helmValuesFile string

var analyzeCmd = &cobra.Command{
	Use:   "analyze <path>",
	Short: "Analyze application configs and generate network policies",
	Long: `Analyze scans a directory for application configuration files,
extracts network dependencies, and generates Kubernetes NetworkPolicy YAML.

The argument can be a local directory path or a GitHub repository URL.
When a GitHub URL is provided, the repository is shallow-cloned to a
temporary directory, analyzed, and then cleaned up automatically.

Supported URL formats:
  - https://github.com/org/repo
  - https://github.com/org/repo.git
  - github.com/org/repo (scheme is added automatically)

Supported file types:
  - Spring: application.yml, application.properties
  - Docker: docker-compose.yml
  - Kubernetes: Deployment, Service, ConfigMap manifests
  - Environment: .env files
  - Build: pom.xml, build.gradle (dependency inference)

AI-powered analysis (--ai flag):
  --ai         Auto-detect: tries local Ollama first, then Gemini cloud
  --ai local   Fully offline analysis via Ollama + NuExtract (ollama pull nuextract)
  --ai cloud   Cloud analysis via Gemini Flash (set GEMINI_API_KEY)`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVar(&aiProvider, "ai", "", "AI backend: 'local' (Ollama), 'cloud' (Gemini), or omit for auto-detect")
	analyzeCmd.Flag("ai").NoOptDefVal = "auto"
	analyzeCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Review dependencies interactively before generating output")
	analyzeCmd.Flags().StringVar(&helmValuesFile, "helm-values", "", "Helm values file to use when rendering charts")
	rootCmd.AddCommand(analyzeCmd)
}

// isGitHubURL reports whether arg looks like a GitHub repository URL.
// It uses proper URL parsing to prevent domain-spoofing attacks like
// github.com.evil.com or evil.github.com.
func isGitHubURL(arg string) bool {
	raw := arg
	// Handle schemeless case: prepend https:// so url.Parse treats the
	// first component as a host rather than a path.
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}

	host := strings.ToLower(u.Hostname())
	return host == "github.com"
}

// normalizeGitHubURL ensures the URL has an https:// scheme.
func normalizeGitHubURL(raw string) string {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	return "https://" + raw
}

// cloneRepo shallow-clones the given git URL into a temp directory and
// returns the directory path. The caller is responsible for removing it.
// A 60-second timeout prevents hanging on unresponsive git servers.
func cloneRepo(url string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "segspec-clone-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	gitCmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", url, tmpDir)
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git clone timed out after 60 seconds: %w", err)
		}
		return "", fmt.Errorf("git clone failed: %w", err)
	}
	return tmpDir, nil
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	path := args[0]

	// If the argument looks like a GitHub URL, clone it first.
	if isGitHubURL(path) {
		url := normalizeGitHubURL(path)
		fmt.Fprintf(os.Stderr, "Cloning %s...\n", url)
		cloneDir, err := cloneRepo(url)
		if err != nil {
			return err
		}
		defer os.RemoveAll(cloneDir)
		path = cloneDir
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	registry := parser.DefaultRegistry()

	walkOpts := walker.WalkOptions{HelmValuesFile: helmValuesFile}
	ds, warnings, err := walker.Walk(path, registry, walkOpts)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d file(s) could not be parsed (use --verbose for details)\n", len(warnings))
	}

	if aiProvider != "" {
		aiDeps, aiErr := ai.Analyze(path, ds.Dependencies(), aiProvider)
		if aiErr != nil {
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

	if interactive {
		if fileInfo, _ := os.Stdout.Stat(); fileInfo.Mode()&os.ModeCharDevice == 0 {
			fmt.Fprintln(os.Stderr, "Warning: --interactive requires a terminal, falling back to non-interactive")
		} else {
			selected, ok := tui.Run(ds.Dependencies())
			if !ok {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return nil
			}
			filtered := model.NewDependencySet(ds.ServiceName)
			for _, dep := range selected {
				filtered.Add(dep)
			}
			ds = filtered
		}
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
	case "per-service":
		fmt.Fprint(out, renderer.PerServiceNetworkPolicy(ds))
	case "all":
		fmt.Fprint(out, renderer.Summary(ds))
		fmt.Fprintln(out, "---")
		fmt.Fprint(out, renderer.NetworkPolicy(ds))
	default:
		return fmt.Errorf("unknown format: %s (valid: summary, netpol, per-service, all)", outputFormat)
	}

	return nil
}
