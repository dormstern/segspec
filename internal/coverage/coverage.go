// Package coverage implements `segspec coverage`: a static cross-check
// between the workloads declared in a project's app configs / K8s manifests
// and the NetworkPolicy / CiliumNetworkPolicy YAML present alongside.
//
// The output answers two operator questions in one shot:
//
//   - "Which of my services have NO NetworkPolicy covering them?" — the
//     coverage gap that Tigera's blog calls out as the operator's first
//     authoring pain ("policies often overwhelm ordinary and veteran
//     users", landscape.md E-005).
//   - "Which of my NetworkPolicies select nothing?" — the orphan-policy
//     side of the same problem; selectors that drift from the workloads
//     they were meant to protect.
//
// This package is read-only and reuses validator.Policy / validator.WorkloadLabels
// from wave-2 so we don't grow a second YAML model. Callers feed it parsed
// inputs; this package never touches the filesystem.
package coverage

import (
	"sort"

	"github.com/dormstern/segspec/internal/validator"
)

// Workload is the coverage-side projection of a single service. It carries
// just enough identity (Name + Namespace + Labels) to render a useful
// report without copying every field of a Deployment/Pod/Compose service.
//
// Source is the origin of the workload: "k8s" for a manifest-derived entry
// or "app-config" for a synthesized entry from a Compose / Spring service
// that has no K8s manifest in the input set.
type Workload struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Source    string
}

// WorkloadCoverage is the per-workload result. MatchedBy lists the policy
// names whose podSelector matches this workload's labels; an empty slice
// means uncovered.
type WorkloadCoverage struct {
	Workload  Workload `json:"workload"`
	Covered   bool     `json:"covered"`
	MatchedBy []string `json:"matched_by,omitempty"`
}

// OrphanPolicy describes a NetworkPolicy whose podSelector matches zero
// workloads in the input set. An empty (select-all) selector is NEVER
// reported as an orphan — that's a deliberate "applies to all in
// namespace" pattern, not a stale reference.
type OrphanPolicy struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// Report is the full coverage result. Workloads is sorted by
// (namespace, name) for deterministic output; OrphanPolicies is sorted by
// (file, line).
type Report struct {
	Workloads        []WorkloadCoverage `json:"workloads"`
	OrphanPolicies   []OrphanPolicy     `json:"orphan_policies"`
	TotalWorkloads   int                `json:"total_workloads"`
	CoveredWorkloads int                `json:"covered_workloads"`
	// Percent is coverage as an integer 0..100. With zero workloads the
	// value is 100 (vacuously covered) — callers that care about empty
	// input check TotalWorkloads first.
	Percent int `json:"coverage_percent"`
}

// Compute walks every workload, asks each policy "does your podSelector
// match my labels?", and aggregates the answers into a Report. It is pure:
// same inputs always produce the same output, with no I/O.
//
// Matching rules mirror validator.CheckUnreferencedAgainst:
//   - An empty PodSelector matches every workload in-namespace (select-all).
//   - When both sides carry a namespace, namespaces must match.
//   - Otherwise the policy's required labels must be a subset of the
//     workload's labels (matchLabels semantics; matchExpressions are not
//     yet modeled).
func Compute(workloads []Workload, policies []validator.Policy) Report {
	rep := Report{
		Workloads:      make([]WorkloadCoverage, 0, len(workloads)),
		OrphanPolicies: []OrphanPolicy{},
		TotalWorkloads: len(workloads),
	}

	// Track whether each policy ever matched a workload, so we can flag
	// orphans on the second pass.
	policyMatched := make([]bool, len(policies))

	for _, w := range workloads {
		wc := WorkloadCoverage{Workload: w}
		for i, p := range policies {
			if !namespaceCompatible(p.Namespace, w.Namespace) {
				continue
			}
			if !labelsMatch(p.PodSelector, w.Labels) {
				continue
			}
			wc.Covered = true
			wc.MatchedBy = append(wc.MatchedBy, p.Name)
			policyMatched[i] = true
		}
		if wc.Covered {
			rep.CoveredWorkloads++
		}
		rep.Workloads = append(rep.Workloads, wc)
	}

	// Orphan pass: policies with a non-empty selector that never matched
	// anything. Empty-selector policies are select-all and are never
	// orphans, even with zero workloads in the input.
	for i, p := range policies {
		if policyMatched[i] {
			continue
		}
		if len(p.PodSelector) == 0 {
			continue
		}
		rep.OrphanPolicies = append(rep.OrphanPolicies, OrphanPolicy{
			Name:      p.Name,
			Namespace: p.Namespace,
			File:      p.File,
			Line:      p.Line,
		})
	}

	// Stable sort outputs. Coverage reports are diffed in CI; deterministic
	// order means the diff highlights real changes rather than reorderings.
	sort.SliceStable(rep.Workloads, func(i, j int) bool {
		a, b := rep.Workloads[i].Workload, rep.Workloads[j].Workload
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	sort.SliceStable(rep.OrphanPolicies, func(i, j int) bool {
		a, b := rep.OrphanPolicies[i], rep.OrphanPolicies[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Name < b.Name
	})

	if rep.TotalWorkloads == 0 {
		rep.Percent = 100
	} else {
		// Integer truncation, not rounding: "75% with 3-of-4 covered" is
		// the contract operators expect from a coverage gate.
		rep.Percent = (rep.CoveredWorkloads * 100) / rep.TotalWorkloads
	}

	return rep
}

// FromValidatorWorkloads adapts the wave-2 validator.WorkloadLabels (which
// has no Name field) into coverage.Workload. The display name is derived
// from common Kubernetes labels in priority order: app.kubernetes.io/name,
// app, name. Falls back to "<unnamed>" so the workload still appears in
// the report rather than being silently dropped.
func FromValidatorWorkloads(in []validator.WorkloadLabels) []Workload {
	out := make([]Workload, 0, len(in))
	for _, w := range in {
		name := nameFromLabels(w.Labels)
		out = append(out, Workload{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    w.Labels,
			Source:    "k8s",
		})
	}
	return out
}

// nameFromLabels returns the most-likely display name from a label map.
// Kept centralized so K8s and app-config workload synthesizers agree on
// the convention.
func nameFromLabels(labels map[string]string) string {
	for _, key := range []string{"app.kubernetes.io/name", "app", "name"} {
		if v, ok := labels[key]; ok && v != "" {
			return v
		}
	}
	return "<unnamed>"
}

// namespaceCompatible mirrors validator.CheckUnreferencedAgainst: when
// both sides carry an explicit namespace, they must match. Either side
// being empty means "no namespace constraint" — common for cluster-scoped
// CiliumClusterwideNetworkPolicy and for synthetic workloads that don't
// carry a namespace yet.
func namespaceCompatible(policyNs, workloadNs string) bool {
	if policyNs == "" || workloadNs == "" {
		return true
	}
	return policyNs == workloadNs
}

// labelsMatch returns true iff every required key/value in the selector
// is present and equal in actual. An empty selector matches everything.
func labelsMatch(required []validator.LabelPair, actual map[string]string) bool {
	for _, lp := range required {
		v, ok := actual[lp.Key]
		if !ok || v != lp.Value {
			return false
		}
	}
	return true
}
