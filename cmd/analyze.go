package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/demo"
	"github.com/dormstern/segspec/internal/ai"
	"github.com/dormstern/segspec/internal/formats"
	"github.com/dormstern/segspec/internal/license"
	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/renderer"
	"github.com/dormstern/segspec/internal/tui"
	"github.com/dormstern/segspec/internal/walker"
)

// gatedFormats maps each paid output format to the license feature flag it
// requires. Formats not in this map (summary, netpol, json) are free forever.
var gatedFormats = map[string]string{
	"evidence":              license.FeatureEvidenceFormat,
	"per-service":           license.FeaturePerServiceFormat,
	"evidence-bundle":       license.FeatureEvidenceFormat,
	"evidence-bundle-sarif": license.FeatureEvidenceFormat,
}

// checkFormatLicense returns an *errLicenseRequired if the requested format
// is gated and the active license doesn't unlock it.
func checkFormatLicense(format string) error {
	feature, gated := gatedFormats[format]
	if !gated {
		return nil
	}
	if license.IsPaidTierAllowed(activeClaims, feature) {
		return nil
	}
	return newLicenseError(
		"--format %s requires a Pro license.\nRun on a public repo or upgrade at https://segspec.dev/pro",
		format,
	)
}

var aiProvider string
var interactive bool
var helmValuesFile string
var demoName string

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
  --ai cloud   Cloud analysis via Gemini Flash (set GEMINI_API_KEY)

Bundled demo fixtures (--demo flag):
  --demo list          List available demo fixtures.
  --demo sentry-mini   Analyze a synthesized Sentry-style multi-service stack.
  --demo microservices-demo  Analyze an adapted microservices-demo k8s manifest.

The --demo flag is mutually exclusive with the positional <path> argument.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().StringVar(&aiProvider, "ai", "", "AI backend: 'local' (Ollama), 'cloud' (Gemini), or omit for auto-detect")
	analyzeCmd.Flag("ai").NoOptDefVal = "auto"
	analyzeCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Review dependencies interactively before generating output")
	analyzeCmd.Flags().StringVar(&helmValuesFile, "helm-values", "", "Helm values file to use when rendering charts")
	analyzeCmd.Flags().StringVar(&demoName, "demo", "", "Analyze a bundled demo fixture instead of a path. Use 'list' to see available demos.")
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

// repoNameFromURL extracts the repository name from a GitHub URL.
// e.g. "https://github.com/PostHog/posthog" -> "posthog"
func repoNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return ""
	}
	name := strings.TrimSuffix(u.Path, ".git")
	name = strings.TrimSuffix(name, "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

// collectInputFiles walks the analyzed directory and returns the (path,
// content) pairs that feed the evidence-bundle's input_tree_sha256. The
// walk mirrors the rules used by the parser walker: skip hidden dirs,
// node_modules, and anything outside the supported config families.
//
// Errors are swallowed individually (a single unreadable file should not
// kill a renderer) but the walk itself stops on a hard error from
// filepath.WalkDir, in which case the partial set of files collected so
// far is returned.
func collectInputFiles(root string) []renderer.EvidenceBundleInputFile {
	var out []renderer.EvidenceBundleInputFile
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "node_modules" || name == ".git" || (len(name) > 1 && strings.HasPrefix(name, ".")) {
				if p != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !isSupportedInputFile(d.Name()) {
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		out = append(out, renderer.EvidenceBundleInputFile{
			Path:    filepath.ToSlash(rel),
			Content: content,
		})
		return nil
	})
	return out
}

// isSupportedInputFile is a small allow-list mirroring the file families
// segspec's parsers care about. Kept conservative: a file we don't parse
// shouldn't influence the input_tree_sha256 (otherwise unrelated repo
// churn would invalidate baselines).
func isSupportedInputFile(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml",
		"application.yml", "application.yaml", "application.properties",
		"pom.xml", "build.gradle", "build.gradle.kts":
		return true
	}
	if strings.HasSuffix(lower, ".env") || lower == ".env" {
		return true
	}
	if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
		return true
	}
	return false
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
	// Resolve --format aliases BEFORE the license check and the format
	// switch: that way a gated alias (e.g. `cilium-network-policy`)
	// canonicalizes to `cilium` and goes through the same code path as
	// the canonical name, with a stderr deprecation warning emitted
	// once. Warnings go to stderr only so rendered YAML on stdout
	// stays pipeable into kubectl apply.
	if canonical, wasAlias := formats.Canonicalize(outputFormat); wasAlias {
		fmt.Fprintf(os.Stderr, "Warning: --format %s is deprecated, use --format %s\n", outputFormat, canonical)
		outputFormat = canonical
	}

	// License gate runs FIRST so we don't waste time cloning, walking, or
	// invoking AI providers for a request that's about to be rejected.
	if err := checkFormatLicense(outputFormat); err != nil {
		return err
	}

	// --demo is mutually exclusive with the positional <path> argument.
	// Combining them would silently prefer one over the other, which is
	// the kind of foot-gun that wastes a beginner's first 60 seconds.
	if demoName != "" && len(args) > 0 {
		return fmt.Errorf("--demo and a positional <path> are mutually exclusive (got --demo %q and path %q)", demoName, args[0])
	}

	// Handle --demo dispatch before the path checks. `--demo list` is a
	// pure listing op; a named demo materializes the embedded fixture
	// into a temp dir and falls through to the normal walker path.
	if demoName != "" {
		if demoName == "list" {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Available demos:")
			for _, d := range demo.Catalog() {
				fmt.Fprintf(out, "  %-20s %s\n", d.Name, d.Description)
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Run: segspec analyze --demo <name>")
			return nil
		}
		resolved, err := demo.Resolve(demoName)
		if err != nil {
			return err
		}
		demoDir, err := resolved.Materialize()
		if err != nil {
			return fmt.Errorf("materialize demo %q: %w", resolved.Name, err)
		}
		defer os.RemoveAll(demoDir)
		args = []string{demoDir}
	}

	if len(args) == 0 {
		return fmt.Errorf("missing path argument (or use --demo <name>; try --demo list)")
	}

	path := args[0]
	repoName := "" // set when cloning a GitHub URL

	// Demo paths get a stable service name in the output rather than the
	// random temp-dir basename ("segspec-demo-sentry-mini-1234567").
	if demoName != "" {
		repoName = demoName
	}

	// If the argument looks like a GitHub URL, clone it first.
	if isGitHubURL(path) {
		url := normalizeGitHubURL(path)
		repoName = repoNameFromURL(url)
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
	// Override temp dir name with repo name in service name and dep sources.
	if repoName != "" {
		ds.RenameSource(ds.ServiceName, repoName)
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
		if outputFormat == "json" {
			fmt.Fprintln(cmd.OutOrStdout(), "{}")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No network dependencies found.")
		}
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
	case "evidence":
		fmt.Fprint(out, renderer.Evidence(ds))
	case "audit":
		fmt.Fprint(out, renderer.Audit(ds))
	case "default-deny":
		fmt.Fprint(out, renderer.DefaultDeny(ds))
	case "cilium":
		fmt.Fprint(out, renderer.Cilium(ds))
	case "json":
		fmt.Fprint(out, renderer.EvidenceJSON(ds))
	case "evidence-bundle":
		fmt.Fprint(out, renderer.EvidenceBundleJSON(ds, Version, collectInputFiles(path), parser.Versions()))
	case "evidence-bundle-sarif":
		fmt.Fprint(out, renderer.EvidenceBundleSARIF(ds, Version, collectInputFiles(path), parser.Versions()))
	default:
		return fmt.Errorf("unknown format: %s (valid: summary, netpol, per-service, all, evidence, audit, default-deny, cilium, json, evidence-bundle, evidence-bundle-sarif)", outputFormat)
	}

	return nil
}
