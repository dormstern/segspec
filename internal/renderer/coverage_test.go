package renderer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/coverage"
)

// TestCoverageText_Empty: the empty report renders the "no workloads
// found" sentinel so callers can dispatch CLI hints off a single line.
func TestCoverageText_Empty(t *testing.T) {
	var buf bytes.Buffer
	CoverageText(&buf, coverage.Report{})
	if !strings.Contains(buf.String(), "no workloads found") {
		t.Errorf("expected 'no workloads found' marker, got %q", buf.String())
	}
}

// TestCoverageText_MixedRendersCounts: the human-readable summary line
// must include the covered/total fraction and percent — CI logs are the
// primary consumer of this format.
func TestCoverageText_MixedRendersCounts(t *testing.T) {
	rep := coverage.Report{
		Workloads: []coverage.WorkloadCoverage{
			{Workload: coverage.Workload{Name: "web", Namespace: "default"}, Covered: true, MatchedBy: []string{"p1"}},
			{Workload: coverage.Workload{Name: "api", Namespace: "default"}, Covered: false},
		},
		OrphanPolicies:   []coverage.OrphanPolicy{{Name: "stale", File: "p.yaml", Line: 1}},
		TotalWorkloads:   2,
		CoveredWorkloads: 1,
		Percent:          50,
	}
	var buf bytes.Buffer
	CoverageText(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "covered    default/web") {
		t.Errorf("expected covered line for web, got %q", out)
	}
	if !strings.Contains(out, "uncovered  default/api") {
		t.Errorf("expected uncovered line for api, got %q", out)
	}
	if !strings.Contains(out, "orphan     stale") {
		t.Errorf("expected orphan line for stale, got %q", out)
	}
	if !strings.Contains(out, "1/2 workloads (50%)") {
		t.Errorf("expected '1/2 workloads (50%%)' summary, got %q", out)
	}
	if !strings.Contains(out, "Orphans: 1") {
		t.Errorf("expected 'Orphans: 1' in summary, got %q", out)
	}
}

// TestCoverageJSON_RoundTrips: the JSON renderer must produce output that
// re-parses into the same Report shape. This is the contract CI ingestors
// rely on.
func TestCoverageJSON_RoundTrips(t *testing.T) {
	in := coverage.Report{
		Workloads: []coverage.WorkloadCoverage{
			{Workload: coverage.Workload{Name: "web", Namespace: "default"}, Covered: true, MatchedBy: []string{"p1"}},
		},
		OrphanPolicies:   []coverage.OrphanPolicy{},
		TotalWorkloads:   1,
		CoveredWorkloads: 1,
		Percent:          100,
	}
	var buf bytes.Buffer
	if err := CoverageJSON(&buf, in); err != nil {
		t.Fatalf("CoverageJSON: %v", err)
	}
	var got coverage.Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v\noutput: %s", err, buf.String())
	}
	if got.TotalWorkloads != in.TotalWorkloads || got.Percent != in.Percent {
		t.Errorf("round-trip mismatch: in=%+v got=%+v", in, got)
	}
	if len(got.Workloads) != 1 || got.Workloads[0].Workload.Name != "web" {
		t.Errorf("expected one workload named web, got %+v", got.Workloads)
	}
}
