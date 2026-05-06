// Package validator implements `segspec validate`: a static linter that
// inspects existing Kubernetes NetworkPolicy and CiliumNetworkPolicy YAML
// for the failure modes operators report — logically-valid-but-broken
// rules that the apiserver happily accepts.
//
// The four checks in this package are derived directly from public issues:
//
//   - MissingDNSEgress       — cilium/cilium#44504 ("toFQDNs without
//     specifying dns rules in egress block for dns service")
//   - ToEntitiesWithToPorts  — cilium/cilium#44504 ("toEntities with
//     toPorts")
//   - OversizedSelectorLabel — cilium/cilium#43771 (k8s 63-char limit on
//     label keys/values; cilium logs but accepts)
//   - UnreferencedSelector   — generic best-practice; flags a podSelector
//     that matches no workload across the input set.
//
// All checks are deterministic, source-only, and emit `file:line` evidence
// per finding. Free tier — this is a sanity check operators run before
// `kubectl apply`, not a paid feature.
package validator

// Severity classifies a finding for reporting purposes. The CLI uses this to
// drive coloring and (eventually) exit codes; for now every severity prints
// in the same report and the binary exits non-zero only on Error.
type Severity string

const (
	SeverityError   Severity = "error"   // policy is broken; will not work as written
	SeverityWarning Severity = "warning" // policy works but has a known footgun
	SeverityInfo    Severity = "info"    // hygiene observation
)

// CheckID is the stable identifier for a validator check. These are part of
// the contract: external tooling (CI, code-scanning) references them.
type CheckID string

const (
	CheckMissingDNSEgress       CheckID = "missing-dns-egress"
	CheckToEntitiesWithToPorts  CheckID = "to-entities-with-to-ports"
	CheckOversizedSelectorLabel CheckID = "oversized-selector-label"
	CheckUnreferencedSelector   CheckID = "unreferenced-selector"
)

// Finding is a single validator hit. The File/Line pair is the evidence
// link operators use to fix the offending YAML.
type Finding struct {
	Check      CheckID  `json:"check"`
	Severity   Severity `json:"severity"`
	Message    string   `json:"message"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	PolicyName string   `json:"policy_name,omitempty"`
	Citation   string   `json:"citation,omitempty"`
}

// Report is the aggregate result of running every check across an input set.
type Report struct {
	Findings []Finding `json:"findings"`
	// PoliciesScanned counts the YAML documents the validator successfully
	// parsed. Zero with no findings means "no policies found" (silent
	// success, per spec).
	PoliciesScanned int `json:"policies_scanned"`
}

// HasErrors returns true when any finding is severity=error. Used by the
// CLI to set a non-zero exit code.
func (r Report) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Run executes every check against the parsed policy set and returns a
// flat aggregated Report. Findings are stable-sorted by (file, line, check)
// for deterministic output.
func Run(policies []Policy) Report {
	var findings []Finding
	findings = append(findings, CheckMissingDNS(policies)...)
	findings = append(findings, CheckToEntitiesToPorts(policies)...)
	findings = append(findings, CheckOversizedLabels(policies)...)
	findings = append(findings, CheckUnreferenced(policies)...)
	sortFindings(findings)
	return Report{Findings: findings, PoliciesScanned: len(policies)}
}
