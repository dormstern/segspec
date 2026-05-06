package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/parser/netpol"
	"github.com/dormstern/segspec/internal/validator"
)

// validateJSON toggles structured JSON output for `segspec validate`. The
// human-readable plain-text report is the default — JSON is meant for CI
// pipes and the future code-scanning ingestor.
var validateJSON bool

var validateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Lint NetworkPolicy / CiliumNetworkPolicy YAML for known footguns",
	Long: `validate inspects existing Kubernetes NetworkPolicy and Cilium
CiliumNetworkPolicy / CiliumClusterwideNetworkPolicy YAML for the
logically-incorrect-but-API-valid patterns the apiserver does not catch.

Unlike 'segspec analyze' (which reads application configs to GENERATE
policies), 'segspec validate' reads NetworkPolicy YAML you already wrote
and checks it for these failure modes:

  - missing-dns-egress         toFQDNs without a DNS-egress rule (cilium #44504)
  - to-entities-with-to-ports  toEntities + toPorts gotcha (cilium #44504)
  - oversized-selector-label   selector key/value > 63 chars (cilium #43771)
  - unreferenced-selector      podSelector matches no workload in the input

Examples:
  segspec validate ./policies/
  segspec validate policy.yaml
  cat policy.yaml | segspec validate -
  segspec validate ./manifests/ --json

Exit codes: 0 = clean (or warnings only); 1 = at least one error-severity
finding (broken policy). Free tier — no license required.`,
	Args: cobra.ExactArgs(1),
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().BoolVar(&validateJSON, "json", false, "Emit findings as JSON instead of plain text")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	path := args[0]

	pr, err := netpol.ReadPath(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Empty input: succeed silently with a hint to stderr. Stdout stays
	// clean so pipelines that grep for findings see nothing — that's the
	// "no policies found" success contract from the wave-2 spec.
	if len(pr.Policies) == 0 {
		fmt.Fprintln(errOut, "no policies found")
		if validateJSON {
			// Even on the empty path, --json must emit a parseable doc so
			// downstream tooling doesn't break on missing input.
			return writeJSON(out, validator.Report{Findings: []validator.Finding{}})
		}
		return nil
	}

	report := validator.RunWithWorkloads(pr.Policies, pr.Workloads)
	if report.Findings == nil {
		report.Findings = []validator.Finding{}
	}

	if validateJSON {
		if err := writeJSON(out, report); err != nil {
			return err
		}
	} else {
		writeText(out, report)
	}

	if report.HasErrors() {
		// Non-zero exit so CI fails the build. errChangesDetected is a
		// shared "stop here, exit 1" sentinel handled by Execute().
		return errChangesDetected
	}
	return nil
}

// writeText prints the human-readable report. Each finding occupies one
// line for greppability: file:line: severity check: message  [citation].
func writeText(w io.Writer, report validator.Report) {
	if len(report.Findings) == 0 {
		fmt.Fprintf(w, "OK — %d policies scanned, no findings\n", report.PoliciesScanned)
		return
	}
	for _, f := range report.Findings {
		fmt.Fprintf(w, "%s:%d: %s %s: %s",
			f.File, f.Line, f.Severity, f.Check, f.Message)
		if f.Citation != "" {
			fmt.Fprintf(w, "  [%s]", f.Citation)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n%d findings across %d policies\n",
		len(report.Findings), report.PoliciesScanned)
}

func writeJSON(w io.Writer, report validator.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
