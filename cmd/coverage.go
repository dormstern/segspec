package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/coverage"
	"github.com/dormstern/segspec/internal/license"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/parser/netpol"
	"github.com/dormstern/segspec/internal/renderer"
	"github.com/dormstern/segspec/internal/validator"
	"github.com/dormstern/segspec/internal/walker"
)

// Coverage subcommand flags. Module-level so tests can reset them between
// runs (mirrors the pattern in validate.go / diff.go).
var (
	coverageJSON      bool
	coverageExitCode  bool
	coverageThreshold int
)

var coverageCmd = &cobra.Command{
	Use:   "coverage <path>",
	Short: "Report which workloads have NetworkPolicies covering them",
	Long: `coverage cross-checks the workloads declared in <path>'s app configs
and Kubernetes manifests against the NetworkPolicy / CiliumNetworkPolicy
YAML in the same path. It answers two questions in one shot:

  - Which services have NO NetworkPolicy covering them?
  - Which NetworkPolicies select zero workloads (orphan policies)?

Examples:
  segspec coverage ./manifests/
  segspec coverage ./repo/ --json
  segspec coverage ./repo/ --exit-code            # fail CI if coverage < 100%
  segspec coverage ./repo/ --exit-code --threshold 80

Free tier: the report itself. Pro tier: the --exit-code CI gate.`,
	Args: cobra.ExactArgs(1),
	RunE: runCoverage,
}

func init() {
	coverageCmd.Flags().BoolVar(&coverageJSON, "json", false, "Emit the report as JSON instead of plain text")
	coverageCmd.Flags().BoolVar(&coverageExitCode, "exit-code", false, "Exit with code 1 when coverage falls below --threshold (Pro license required)")
	coverageCmd.Flags().IntVar(&coverageThreshold, "threshold", 100, "Minimum acceptable coverage percentage when --exit-code is set (0-100)")
	rootCmd.AddCommand(coverageCmd)
}

func runCoverage(cmd *cobra.Command, args []string) error {
	// License gate runs FIRST so we don't waste time walking a large repo
	// for a request that's about to be rejected. Mirrors diff --exit-code.
	if coverageExitCode && !license.IsPaidTierAllowed(activeClaims, license.FeatureExitCode) {
		return newLicenseError(
			"--exit-code requires a Pro license.\nRun on a public repo or upgrade at https://segspec.dev/pro",
		)
	}
	if coverageThreshold < 0 || coverageThreshold > 100 {
		return fmt.Errorf("--threshold must be between 0 and 100, got %d", coverageThreshold)
	}

	path := args[0]
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Step 1: parse any NetworkPolicy / CiliumNetworkPolicy + K8s workload
	// labels under <path>. ReadPath swallows per-file parse failures so a
	// single broken document doesn't kill the whole report.
	pr, err := netpol.ReadPath(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Step 2: walk app configs (Spring, Compose, Helm, K8s) for additional
	// service identities. K8s workloads with labels are already covered by
	// pr.Workloads; this step adds Spring / Compose services that have no
	// matching K8s manifest, synthesizing an `app=<name>` label.
	appWorkloads := collectAppConfigWorkloads(path)

	workloads := mergeWorkloads(pr.Workloads, appWorkloads)

	// Empty input contract: succeed with a "no workloads found" hint on
	// stderr so pipelines that grep stdout for findings see nothing. JSON
	// callers still get a valid empty report on stdout.
	if len(workloads) == 0 {
		if coverageJSON {
			return renderer.CoverageJSON(out, coverage.Compute(nil, nil))
		}
		fmt.Fprintln(errOut, "no workloads found")
		return nil
	}

	rep := coverage.Compute(workloads, pr.Policies)

	if coverageJSON {
		if err := renderer.CoverageJSON(out, rep); err != nil {
			return err
		}
	} else {
		renderer.CoverageText(out, rep)
	}

	if coverageExitCode && rep.Percent < coverageThreshold {
		// SilenceErrors prevents Execute() from printing a generic error
		// banner — the report body has already been printed and the
		// non-zero exit IS the signal CI is looking for.
		cmd.SilenceErrors = true
		return errChangesDetected
	}
	return nil
}

// collectAppConfigWorkloads runs the standard analyzer walker against
// path and synthesizes a coverage.Workload per discovered service name.
// These complement the K8s-manifest workloads parsed by netpol.ReadPath
// for projects whose source-of-truth is Compose or Spring rather than raw
// manifests.
func collectAppConfigWorkloads(path string) []coverage.Workload {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		// netpol.ReadPath already validated the path; if walker can't run
		// (e.g., single-file input) we just skip the app-config layer and
		// rely on the K8s manifest workloads.
		return nil
	}
	registry := parser.DefaultRegistry()
	ds, _, err := walker.Walk(path, registry)
	if err != nil || ds == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []coverage.Workload{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		// Skip the implicit umbrella "service" the walker uses for the
		// repo root — it's a directory name, not a workload identity.
		if name == filepath.Base(path) {
			return
		}
		seen[name] = true
		out = append(out, coverage.Workload{
			Name:   name,
			Labels: map[string]string{"app": name},
			Source: "app-config",
		})
	}
	for _, dep := range ds.Dependencies() {
		add(dep.Source)
		add(dep.Target)
	}
	return out
}

// mergeWorkloads concatenates the K8s-manifest workloads (which already
// carry real labels) with the app-config-synthesized ones, dropping any
// app-config entry whose synthesized `app=<name>` would shadow an
// already-present K8s workload of the same display name.
func mergeWorkloads(k8sWorkloads []validator.WorkloadLabels, appWorkloads []coverage.Workload) []coverage.Workload {
	merged := coverage.FromValidatorWorkloads(k8sWorkloads)

	have := map[string]bool{}
	for _, w := range merged {
		have[w.Name] = true
	}
	for _, w := range appWorkloads {
		if have[w.Name] {
			continue
		}
		merged = append(merged, w)
	}
	return merged
}
