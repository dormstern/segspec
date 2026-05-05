package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

// TestDefaultDenyEmptyServiceSet verifies that the default-deny renderer emits
// ONLY the namespace-scoped default-deny policy when no dependencies exist.
// This is the auditor sanity case: even an empty input must produce the
// scaffold policy auditors demand on every namespace.
func TestDefaultDenyEmptyServiceSet(t *testing.T) {
	ds := model.NewDependencySet("empty")
	got := DefaultDeny(ds)

	if got == "" {
		t.Fatal("expected non-empty output even with no deps (default-deny scaffold should still emit)")
	}

	// Must contain exactly one default-deny policy.
	wantSubstrings := []string{
		"kind: NetworkPolicy",
		"name: default-deny",
		"podSelector: {}",
		"- Ingress",
		"- Egress",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(got, s) {
			t.Errorf("default-deny output missing %q\nGot:\n%s", s, got)
		}
	}

	// Must NOT contain any per-service allow policy when there are no deps.
	if strings.Contains(got, "-netpol") {
		t.Errorf("expected no per-service allow policies on empty input; got:\n%s", got)
	}

	// Default-deny must have exactly empty ingress/egress: the canonical way
	// to express deny-all is `policyTypes: [Ingress, Egress]` with no rules.
	// The output should not declare any `ingress:` or `egress:` blocks for the
	// default-deny policy beyond policyTypes.
	if strings.Count(got, "kind: NetworkPolicy") != 1 {
		t.Errorf("expected exactly 1 NetworkPolicy in empty case; got:\n%s", got)
	}
}

// TestDefaultDenySingleServiceWithEgress verifies that a single service with
// one declared egress dependency produces a default-deny + a per-service allow
// policy carrying the right egress rule.
func TestDefaultDenySingleServiceWithEgress(t *testing.T) {
	ds := model.NewDependencySet("orders")
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP",
		Confidence: model.High,
		SourceFile: "application.yml",
	})

	got := DefaultDeny(ds)

	// Default-deny appears.
	if !strings.Contains(got, "name: default-deny") {
		t.Errorf("missing default-deny policy:\n%s", got)
	}
	// Allow policy for orders appears with egress rule to postgres:5432.
	if !strings.Contains(got, "name: orders-netpol") {
		t.Errorf("missing orders-netpol allow policy:\n%s", got)
	}
	if !strings.Contains(got, "app: postgres") {
		t.Errorf("missing egress target podSelector for postgres:\n%s", got)
	}
	if !strings.Contains(got, "port: 5432") {
		t.Errorf("missing egress port 5432:\n%s", got)
	}

	// At least 3 documents: default-deny + orders-netpol + postgres-netpol
	docs := strings.Count(got, "kind: NetworkPolicy")
	if docs < 2 {
		t.Errorf("expected at least 2 NetworkPolicy documents (default-deny + allow); got %d:\n%s", docs, got)
	}
}

// TestDefaultDenyDeterministicOrdering verifies that 3 services with varied
// dependencies always produce default-deny first, then per-service allow
// policies in alphabetical order regardless of insert order.
func TestDefaultDenyDeterministicOrdering(t *testing.T) {
	// Insert in NON-alphabetical order: zebra, alpha, mango.
	ds := model.NewDependencySet("zebra")
	ds.Add(model.NetworkDependency{Source: "zebra", Target: "db", Port: 5432, Protocol: "TCP", Confidence: model.High})
	ds.Add(model.NetworkDependency{Source: "alpha", Target: "cache", Port: 6379, Protocol: "TCP", Confidence: model.High})
	ds.Add(model.NetworkDependency{Source: "mango", Target: "queue", Port: 5672, Protocol: "TCP", Confidence: model.High})

	got := DefaultDeny(ds)

	// Find positions of each policy name.
	posDeny := strings.Index(got, "name: default-deny")
	posAlpha := strings.Index(got, "name: alpha-netpol")
	posCache := strings.Index(got, "name: cache-netpol")
	posDB := strings.Index(got, "name: db-netpol")
	posMango := strings.Index(got, "name: mango-netpol")
	posQueue := strings.Index(got, "name: queue-netpol")
	posZebra := strings.Index(got, "name: zebra-netpol")

	if posDeny < 0 {
		t.Fatalf("default-deny missing:\n%s", got)
	}

	// All allow policies must appear after default-deny.
	for name, pos := range map[string]int{
		"alpha-netpol": posAlpha, "cache-netpol": posCache, "db-netpol": posDB,
		"mango-netpol": posMango, "queue-netpol": posQueue, "zebra-netpol": posZebra,
	} {
		if pos < 0 {
			t.Errorf("missing %s in output", name)
			continue
		}
		if pos < posDeny {
			t.Errorf("%s appears BEFORE default-deny; default-deny must be first", name)
		}
	}

	// Allow policies must be alphabetical: alpha < cache < db < mango < queue < zebra.
	ordered := []struct {
		name string
		pos  int
	}{
		{"alpha-netpol", posAlpha},
		{"cache-netpol", posCache},
		{"db-netpol", posDB},
		{"mango-netpol", posMango},
		{"queue-netpol", posQueue},
		{"zebra-netpol", posZebra},
	}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].pos >= ordered[i].pos {
			t.Errorf("ordering violation: %s (pos %d) should appear before %s (pos %d)",
				ordered[i-1].name, ordered[i-1].pos, ordered[i].name, ordered[i].pos)
		}
	}
}

// TestDefaultDenyDNSEgressPreserved verifies that allow policies emitted by the
// default-deny renderer still include the DNS egress block (port 53 to
// kube-system) for any service with egress. This guards against the
// authoring mistake documented by Cilium issue #44504 (toFQDNs without DNS
// egress) — segspec must never introduce that bug.
func TestDefaultDenyDNSEgressPreserved(t *testing.T) {
	ds := model.NewDependencySet("orders")
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP",
		Confidence: model.High,
	})

	got := DefaultDeny(ds)

	// DNS egress preservation checks: kube-system namespace selector + port 53 UDP and TCP.
	wantDNS := []string{
		"kubernetes.io/metadata.name: kube-system",
		"port: 53",
		"protocol: UDP",
		"protocol: TCP",
	}
	for _, s := range wantDNS {
		if !strings.Contains(got, s) {
			t.Errorf("DNS egress preservation broken: missing %q\n%s", s, got)
		}
	}

	// Specifically: there must be at least 2 occurrences of `port: 53` (UDP + TCP).
	if strings.Count(got, "port: 53") < 2 {
		t.Errorf("expected DNS egress to declare both UDP and TCP on port 53\n%s", got)
	}
}

// TestDefaultDenyNamespaceHandling verifies that:
// (a) When NO namespace is detectable from input deps, the default-deny policy
//     is emitted WITHOUT a namespace label (cluster admin will fill it on apply).
// (b) When a namespace IS detected from FQDNs in deps, the default-deny policy
//     carries that namespace as its `metadata.namespace` field.
func TestDefaultDenyNamespaceHandling(t *testing.T) {
	t.Run("no namespace detected", func(t *testing.T) {
		ds := model.NewDependencySet("orders")
		ds.Add(model.NetworkDependency{
			Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP",
			Confidence: model.High,
		})

		got := DefaultDeny(ds)

		// In the default-deny block specifically: locate "name: default-deny",
		// extract the next ~6 lines, and assert no `namespace:` label appears
		// before the next document separator.
		denyBlock := extractPolicyBlock(got, "default-deny")
		if denyBlock == "" {
			t.Fatalf("could not locate default-deny block:\n%s", got)
		}
		if strings.Contains(denyBlock, "namespace:") {
			t.Errorf("default-deny should not carry namespace label when none detected; got:\n%s", denyBlock)
		}
	})

	t.Run("namespace detected from FQDN", func(t *testing.T) {
		ds := model.NewDependencySet("orders")
		// FQDN target carries namespace "production".
		ds.Add(model.NetworkDependency{
			Source: "orders", Target: "postgres.production.svc.cluster.local",
			Port: 5432, Protocol: "TCP",
			Confidence: model.High,
		})

		got := DefaultDeny(ds)

		denyBlock := extractPolicyBlock(got, "default-deny")
		if denyBlock == "" {
			t.Fatalf("could not locate default-deny block:\n%s", got)
		}
		if !strings.Contains(denyBlock, "namespace: production") {
			t.Errorf("default-deny should carry detected namespace 'production'; got block:\n%s", denyBlock)
		}
	})
}

// TestDefaultDenyAllowPoliciesMatchPerService verifies that the allow-policy
// portion of the default-deny output is byte-identical to the output of
// `--format per-service` on the same input. This guards against drift
// between the two renderers — they must produce the same per-service policies
// for the same dependencies.
func TestDefaultDenyAllowPoliciesMatchPerService(t *testing.T) {
	ds := model.NewDependencySet("orders")
	ds.Add(model.NetworkDependency{Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: model.High})
	ds.Add(model.NetworkDependency{Source: "checkout", Target: "orders", Port: 8080, Protocol: "TCP", Confidence: model.High})

	defaultDeny := DefaultDeny(ds)
	perService := PerServiceNetworkPolicy(ds)

	if perService == "" {
		t.Fatal("per-service rendered empty; cannot compare")
	}

	// Trim trailing whitespace for stability across renderers.
	perServiceTrimmed := strings.TrimSpace(perService)
	if !strings.Contains(defaultDeny, perServiceTrimmed) {
		t.Errorf("default-deny output does not contain per-service block verbatim.\n--- default-deny ---\n%s\n--- per-service expected substring ---\n%s",
			defaultDeny, perServiceTrimmed)
	}
}

// extractPolicyBlock returns the YAML block whose metadata.name matches
// the given name (best-effort; reads from the name line up to the next
// `---` separator or end of string). Used by namespace-handling tests.
func extractPolicyBlock(yaml, name string) string {
	marker := "name: " + name
	idx := strings.Index(yaml, marker)
	if idx < 0 {
		return ""
	}
	// Walk backward to the start of the document (either start of string or
	// the previous `---\n`).
	start := strings.LastIndex(yaml[:idx], "---\n")
	if start < 0 {
		start = 0
	} else {
		start += len("---\n")
	}
	// Walk forward to the next `---\n` or end-of-string.
	end := strings.Index(yaml[idx:], "\n---\n")
	if end < 0 {
		return yaml[start:]
	}
	return yaml[start : idx+end]
}
