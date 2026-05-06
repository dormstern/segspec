package renderer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dormstern/segspec/internal/coverage"
)

// CoverageText writes a human-readable coverage table to w. Layout is
// optimized for a terminal-first audience: a single workload-by-workload
// block, an orphan-policy block when one exists, and a one-line summary.
//
// The format intentionally mirrors `segspec validate`: every line starts
// with a stable token (covered/uncovered/orphan) so CI scripts can grep
// for findings without parsing JSON.
func CoverageText(w io.Writer, rep coverage.Report) {
	if rep.TotalWorkloads == 0 {
		// Empty input is a successful state: pipelines should not see noise
		// on stdout, so we mirror `validate`'s "no policies found" pattern
		// and emit the hint on the caller's stderr instead. The coverage
		// command surfaces that hint; the renderer just keeps stdout quiet.
		fmt.Fprintln(w, "no workloads found")
		return
	}

	for _, wc := range rep.Workloads {
		ns := wc.Workload.Namespace
		if ns == "" {
			ns = "-"
		}
		if wc.Covered {
			fmt.Fprintf(w, "covered    %s/%s  by [%s]\n",
				ns, wc.Workload.Name, strings.Join(wc.MatchedBy, ", "))
		} else {
			fmt.Fprintf(w, "uncovered  %s/%s\n", ns, wc.Workload.Name)
		}
	}

	if len(rep.OrphanPolicies) > 0 {
		fmt.Fprintln(w)
		for _, op := range rep.OrphanPolicies {
			fmt.Fprintf(w, "orphan     %s  (%s:%d)\n", op.Name, op.File, op.Line)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Coverage: %d/%d workloads (%d%%)  Orphans: %d\n",
		rep.CoveredWorkloads, rep.TotalWorkloads, rep.Percent, len(rep.OrphanPolicies))
}

// CoverageJSON writes the report as indented JSON. Used by `--json` for CI
// ingestion: the schema is the coverage.Report struct itself, so adding
// fields requires an explicit deprecation cycle (see contracts.md).
func CoverageJSON(w io.Writer, rep coverage.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}
