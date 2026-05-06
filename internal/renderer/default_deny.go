package renderer

import (
	"fmt"
	"net"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

// DefaultDeny renders a multi-document YAML containing:
//
//  1. A namespace-scoped default-deny NetworkPolicy (podSelector: {} +
//     policyTypes [Ingress, Egress] with no rules — denies everything).
//  2. The per-service allow policies produced by PerServiceNetworkPolicy(),
//     one per discovered service, alphabetically sorted.
//
// This single command satisfies the auditor demand documented in
// cilium/cilium #43502 / #43503: "every deployment must have a NetworkPolicy",
// where deny-by-default is the floor and explicit per-workload allow-rules
// are the override. Reusing PerServiceNetworkPolicy guarantees the allow
// policies are byte-identical to `--format per-service` for the same input,
// so both formats stay in lockstep.
//
// Namespace handling:
//   - If a namespace is detectable from any FQDN target/source in the input
//     (e.g. `postgres.production.svc.cluster.local` -> "production"), the
//     default-deny policy carries `metadata.namespace: <detected>`.
//   - If no namespace is detectable, the default-deny policy is emitted
//     without a namespace field — the cluster admin fills it on apply.
//     This mirrors the pattern used by ahmetb/network-policy-recipes and
//     keeps segspec's output cluster-agnostic by default.
func DefaultDeny(ds *model.DependencySet) string {
	var b strings.Builder

	namespace := detectNamespace(ds)

	// --- Document 1: default-deny ----------------------------------------
	b.WriteString("# Default-deny scaffold for the namespace.\n")
	b.WriteString("# Auditor demand: cilium/cilium#43502 / #43503 — every workload must\n")
	b.WriteString("# be covered by a NetworkPolicy. This policy denies all ingress and\n")
	b.WriteString("# egress; the allow policies below override it for declared deps.\n")
	b.WriteString("apiVersion: networking.k8s.io/v1\n")
	b.WriteString("kind: NetworkPolicy\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: default-deny\n")
	if namespace != "" {
		fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	}
	b.WriteString("  labels:\n")
	b.WriteString("    generated-by: segspec\n")
	b.WriteString("spec:\n")
	b.WriteString("  podSelector: {}\n")
	b.WriteString("  policyTypes:\n")
	b.WriteString("    - Ingress\n")
	b.WriteString("    - Egress\n")

	// --- Documents 2..N: per-service allow policies -----------------------
	// Reuse PerServiceNetworkPolicy verbatim so the two formats never drift.
	allow := PerServiceNetworkPolicy(ds)
	if allow != "" {
		b.WriteString("---\n")
		b.WriteString(allow)
	}

	return b.String()
}

// detectNamespace inspects every dependency in the set looking for an FQDN
// of the form "service.namespace.svc.cluster.local" or "service.namespace".
// Returns the most common namespace encountered, or "" if none are detectable.
//
// The heuristic: split each Source/Target on `.`, treat parts[1] as namespace,
// skip values that look like IP addresses or that lack at least 2 dot-separated
// segments. If multiple namespaces appear, the first one (sorted by Key()) wins
// for determinism — the rationale being that segspec is per-namespace by
// design, and a multi-namespace input is an authoring smell the operator
// should resolve, not a feature we paper over.
func detectNamespace(ds *model.DependencySet) string {
	for _, dep := range ds.Dependencies() {
		if ns := nsFromHost(dep.Target); ns != "" {
			return ns
		}
		if ns := nsFromHost(dep.Source); ns != "" {
			return ns
		}
	}
	return ""
}

// nsFromHost extracts the namespace segment from a dot-separated hostname.
// Returns "" if the input is empty, a literal IP, or has fewer than 2 segments.
func nsFromHost(host string) string {
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return ""
	}
	ns := parts[1]
	// Filter obviously-non-namespace segments (e.g. TLDs from external hosts
	// like "api.stripe.com"). Kubernetes namespaces are DNS-1123 labels;
	// reject anything containing a TLD-shaped trailing pattern by requiring
	// the third segment (if present) to be a Kubernetes-cluster-shaped marker.
	if len(parts) >= 3 {
		switch parts[2] {
		case "svc", "cluster", "local":
			return ns
		}
		// External FQDN like api.stripe.com — not a namespace.
		return ""
	}
	// Two-segment host: treat parts[1] as namespace (matches the existing
	// FQDN handling in renderEgressTo when len(parts) == 2).
	return ns
}
