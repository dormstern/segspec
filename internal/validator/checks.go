package validator

import (
	"fmt"
	"sort"
	"strings"
)

// k8sLabelMaxLen is the Kubernetes label key/value length limit. Cilium
// logs an error when this is exceeded but does NOT reject the policy —
// see cilium/cilium#43771.
const k8sLabelMaxLen = 63

// Policy is the validator's normalized view of a single parsed YAML
// document — either a vanilla Kubernetes NetworkPolicy or a Cilium
// CiliumNetworkPolicy / CiliumClusterwideNetworkPolicy. It deliberately
// keeps only the fields the four checks actually inspect; expanding
// coverage means adding fields here, not reaching into raw YAML at the
// check site.
type Policy struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string

	// File and Line locate the YAML document for evidence linking.
	File string
	Line int

	// PodSelector is the policy's primary workload selector (.spec.podSelector
	// for vanilla NetworkPolicy, .spec.endpointSelector for Cilium). Stored
	// as flat key=value strings so checks don't have to know about MatchLabels
	// vs. MatchExpressions.
	PodSelector []LabelPair

	// Egress rules. Vanilla NetworkPolicy egress carries Ports + To peers;
	// Cilium egress additionally carries ToFQDNs and ToEntities.
	Egress []EgressRule

	// Ingress rules — kept minimal; only label-length checks reach in here
	// today.
	Ingress []IngressRule
}

// LabelPair is one selector key/value with its source line. Keeping the
// line per-pair lets OversizedSelectorLabel point at the exact offending
// label inside a multi-line selector.
type LabelPair struct {
	Key   string
	Value string
	Line  int
}

// EgressRule captures the subset of an egress block the validator inspects.
type EgressRule struct {
	Line       int
	HasToPorts bool
	// ToPortsPorts is the parsed port numbers from .toPorts[].ports[].port.
	// Used by CheckMissingDNS to recognize an explicit DNS-egress rule.
	ToPortsPorts []int
	ToFQDNs      []FQDNTarget
	ToEntities   []string
	// To is the K8s "to" peer list — only podSelector keys are tracked, for
	// the unreferenced-selector cross-check.
	To []PeerSelector
}

// IngressRule mirrors EgressRule for the ingress side; today we only walk
// it for label length.
type IngressRule struct {
	Line int
	From []PeerSelector
}

// PeerSelector is a single peer in a to/from list.
type PeerSelector struct {
	PodSelector []LabelPair
	Line        int
}

// FQDNTarget is one entry in a Cilium toFQDNs list.
type FQDNTarget struct {
	MatchName    string
	MatchPattern string
	Line         int
}

// CheckMissingDNS implements MissingDNSEgress: a policy that uses toFQDNs
// without a sibling egress rule allowing UDP/TCP 53 will silently fail at
// runtime because Cilium's FQDN resolver cannot observe the DNS lookup.
//
// Citation: cilium/cilium#44504 — "toFQDNs without specifying dns rules in
// egress block for dns service".
func CheckMissingDNS(policies []Policy) []Finding {
	var out []Finding
	for _, p := range policies {
		var fqdnLines []int
		hasDNSPort := false
		for _, e := range p.Egress {
			for _, f := range e.ToFQDNs {
				fqdnLines = append(fqdnLines, f.Line)
			}
			for _, port := range e.ToPortsPorts {
				if port == 53 {
					hasDNSPort = true
				}
			}
		}
		if len(fqdnLines) == 0 || hasDNSPort {
			continue
		}
		// Report a single finding per policy at the first toFQDNs line —
		// one fix point, not N noisy findings.
		sort.Ints(fqdnLines)
		out = append(out, Finding{
			Check:      CheckMissingDNSEgress,
			Severity:   SeverityError,
			Message:    fmt.Sprintf("policy %q uses toFQDNs but has no egress rule allowing DNS (port 53); FQDN resolution will fail silently", p.Name),
			File:       p.File,
			Line:       fqdnLines[0],
			PolicyName: p.Name,
			Citation:   "https://github.com/cilium/cilium/issues/44504",
		})
	}
	return out
}

// CheckToEntitiesToPorts implements ToEntitiesWithToPorts: combining a
// Cilium toEntities block with toPorts is a known footgun — the
// to-entities matcher applies at L3 and silently drops the L4 port
// constraint, so the policy is more permissive than it reads.
//
// Citation: cilium/cilium#44504 — "toEntities with toPorts".
func CheckToEntitiesToPorts(policies []Policy) []Finding {
	var out []Finding
	for _, p := range policies {
		for _, e := range p.Egress {
			if len(e.ToEntities) == 0 || !e.HasToPorts {
				continue
			}
			out = append(out, Finding{
				Check:      CheckToEntitiesWithToPorts,
				Severity:   SeverityWarning,
				Message:    fmt.Sprintf("policy %q combines toEntities (%s) with toPorts; the port constraint will not be enforced as written", p.Name, strings.Join(e.ToEntities, ",")),
				File:       p.File,
				Line:       e.Line,
				PolicyName: p.Name,
				Citation:   "https://github.com/cilium/cilium/issues/44504",
			})
		}
	}
	return out
}

// CheckOversizedLabels implements OversizedSelectorLabel: any selector
// label key or value longer than 63 chars exceeds the k8s limit. Cilium
// logs an error but accepts the policy (cilium/cilium#43771); the result
// is operator confusion ("everything works (seemingly) fine").
//
// Severity is Error rather than Warning: the policy is broken on
// upstream Kubernetes regardless of CNI.
func CheckOversizedLabels(policies []Policy) []Finding {
	var out []Finding
	for _, p := range policies {
		walk := func(pairs []LabelPair) {
			for _, lp := range pairs {
				if len(lp.Key) > k8sLabelMaxLen {
					out = append(out, Finding{
						Check:      CheckOversizedSelectorLabel,
						Severity:   SeverityError,
						Message:    fmt.Sprintf("policy %q selector key %q is %d chars (max %d)", p.Name, lp.Key, len(lp.Key), k8sLabelMaxLen),
						File:       p.File,
						Line:       lp.Line,
						PolicyName: p.Name,
						Citation:   "https://github.com/cilium/cilium/issues/43771",
					})
				}
				if len(lp.Value) > k8sLabelMaxLen {
					out = append(out, Finding{
						Check:      CheckOversizedSelectorLabel,
						Severity:   SeverityError,
						Message:    fmt.Sprintf("policy %q selector value for %q is %d chars (max %d)", p.Name, lp.Key, len(lp.Value), k8sLabelMaxLen),
						File:       p.File,
						Line:       lp.Line,
						PolicyName: p.Name,
						Citation:   "https://github.com/cilium/cilium/issues/43771",
					})
				}
			}
		}
		walk(p.PodSelector)
		for _, e := range p.Egress {
			for _, peer := range e.To {
				walk(peer.PodSelector)
			}
		}
		for _, in := range p.Ingress {
			for _, peer := range in.From {
				walk(peer.PodSelector)
			}
		}
	}
	return out
}

// CheckUnreferenced implements UnreferencedSelector: when the input set
// contains both policies and workload manifests, every policy's
// podSelector should match at least one workload. A selector that matches
// nothing is almost always a typo or a stale reference.
//
// Workloads are detected via WorkloadLabels passed in alongside policies.
// If no workloads are supplied at all (validate of a policy-only
// directory), this check is a no-op — we don't have ground truth.
func CheckUnreferenced(policies []Policy) []Finding {
	// Pure-policy view first; integration tests pass workloads via the
	// RunWithWorkloads variant. When called from Run, policies-only mode
	// short-circuits cleanly.
	return CheckUnreferencedAgainst(policies, nil)
}

// CheckUnreferencedAgainst is the workload-aware variant. Findings are
// emitted only when len(workloads) > 0; otherwise the validator has no
// ground truth and stays silent.
func CheckUnreferencedAgainst(policies []Policy, workloads []WorkloadLabels) []Finding {
	if len(workloads) == 0 {
		return nil
	}
	var out []Finding
	for _, p := range policies {
		if len(p.PodSelector) == 0 {
			// An empty selector matches everything in-namespace — that's
			// a deliberate "select all" and not unreferenced.
			continue
		}
		matched := false
		for _, w := range workloads {
			if w.Namespace != "" && p.Namespace != "" && w.Namespace != p.Namespace {
				continue
			}
			if labelsMatch(p.PodSelector, w.Labels) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		out = append(out, Finding{
			Check:      CheckUnreferencedSelector,
			Severity:   SeverityWarning,
			Message:    fmt.Sprintf("policy %q podSelector matches no workload in the input set", p.Name),
			File:       p.File,
			Line:       p.Line,
			PolicyName: p.Name,
			Citation:   "best-practice — no upstream issue, dead-policy hygiene",
		})
	}
	return out
}

// WorkloadLabels is the projection of a Deployment / DaemonSet / Pod we
// need to evaluate selector references. Only label maps and namespace
// matter for the cross-check.
type WorkloadLabels struct {
	Namespace string
	Labels    map[string]string
}

// labelsMatch returns true if every required label is present and equal
// in actual.
func labelsMatch(required []LabelPair, actual map[string]string) bool {
	for _, lp := range required {
		v, ok := actual[lp.Key]
		if !ok || v != lp.Value {
			return false
		}
	}
	return true
}

// sortFindings stabilizes report output across runs.
func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		if f[i].Line != f[j].Line {
			return f[i].Line < f[j].Line
		}
		return string(f[i].Check) < string(f[j].Check)
	})
}

// RunWithWorkloads is the workload-aware top-level entrypoint.
func RunWithWorkloads(policies []Policy, workloads []WorkloadLabels) Report {
	var findings []Finding
	findings = append(findings, CheckMissingDNS(policies)...)
	findings = append(findings, CheckToEntitiesToPorts(policies)...)
	findings = append(findings, CheckOversizedLabels(policies)...)
	findings = append(findings, CheckUnreferencedAgainst(policies, workloads)...)
	sortFindings(findings)
	return Report{Findings: findings, PoliciesScanned: len(policies)}
}
