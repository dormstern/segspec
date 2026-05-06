package renderer

import (
	"os"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser/netpol"
	"github.com/dormstern/segspec/internal/validator"
)

// --- Helpers ----------------------------------------------------------------

// parseAndValidate writes the rendered YAML to a tempfile, lets the netpol
// parser ingest it, and runs the static validator. Returns the report so
// individual tests can assert on findings.
func parseAndValidate(t *testing.T, yamlText string) validator.Report {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/policy.yaml"
	if err := writeFile(path, yamlText); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}
	pr, err := netpol.ReadPath(path)
	if err != nil {
		t.Fatalf("netpol.ReadPath: %v", err)
	}
	return validator.Run(pr.Policies)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// --- Tests ------------------------------------------------------------------

// 1. Empty deps → empty output.
func TestCilium_EmptyDeps(t *testing.T) {
	ds := model.NewDependencySet("empty")
	out := Cilium(ds)
	if out != "" {
		t.Errorf("expected empty output for no deps, got %q", out)
	}
}

// 2. In-cluster service dep → endpointSelector + egress rule using
//    endpointSelector-shaped peer (NOT toFQDNs).
func TestCilium_InClusterService(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Confidence: model.High,
	})
	out := Cilium(ds)
	wants := []string{
		"apiVersion: cilium.io/v2",
		"kind: CiliumNetworkPolicy",
		"endpointSelector:",
		"app: order-service",
		"egress:",
		"toEndpoints:",
		"app: postgres",
		"toPorts:",
		"port: \"5432\"",
		"protocol: TCP",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\n%s", w, out)
		}
	}
	// Must NOT use podSelector (vanilla NP shape) for the workload anchor.
	if strings.Contains(out, "podSelector:") {
		t.Errorf("expected endpointSelector, found podSelector:\n%s", out)
	}
	// In-cluster shapes must NOT emit toFQDNs.
	if strings.Contains(out, "toFQDNs:") {
		t.Errorf("in-cluster target should not emit toFQDNs:\n%s", out)
	}
}

// 3. External FQDN dep → toFQDNs[matchName] + paired DNS egress block.
func TestCilium_ExternalFQDNAddsDNSEgress(t *testing.T) {
	ds := model.NewDependencySet("api")
	ds.Add(model.NetworkDependency{
		Source: "api", Target: "api.stripe.com", Port: 443, Protocol: "TCP",
		Confidence: model.High,
	})
	out := Cilium(ds)
	wants := []string{
		"toFQDNs:",
		"matchName: \"api.stripe.com\"",
		// DNS egress block paired with any FQDN rule:
		"k8s:io.kubernetes.pod.namespace: kube-system",
		"port: \"53\"",
		"protocol: UDP",
		"rules:",
		"dns:",
		"matchPattern: \"*\"",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\n%s", w, out)
		}
	}
}

// 4. Multi-FQDN policy → ONE paired DNS egress block (not duplicated per FQDN).
func TestCilium_MultiFQDNSingleDNSBlock(t *testing.T) {
	ds := model.NewDependencySet("api")
	ds.Add(model.NetworkDependency{Source: "api", Target: "api.stripe.com", Port: 443, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "api", Target: "api.github.com", Port: 443, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "api", Target: "hooks.slack.com", Port: 443, Protocol: "TCP"})
	out := Cilium(ds)
	// Three matchName entries.
	if got := strings.Count(out, "matchName:"); got != 3 {
		t.Errorf("expected 3 matchName entries, got %d:\n%s", got, out)
	}
	// Exactly one DNS egress block — counted by the kube-system namespace
	// label which is unique to that block.
	if got := strings.Count(out, "k8s:io.kubernetes.pod.namespace: kube-system"); got != 1 {
		t.Errorf("expected exactly 1 paired DNS egress block, got %d:\n%s", got, out)
	}
}

// 5. Wildcard FQDN → uses matchPattern, not matchName.
func TestCilium_WildcardFQDNUsesMatchPattern(t *testing.T) {
	ds := model.NewDependencySet("worker")
	ds.Add(model.NetworkDependency{
		Source: "worker", Target: "*.amazonaws.com", Port: 443, Protocol: "TCP",
	})
	out := Cilium(ds)
	if !strings.Contains(out, "matchPattern: \"*.amazonaws.com\"") {
		t.Errorf("wildcard FQDN should use matchPattern:\n%s", out)
	}
	// And specifically NOT matchName for that target.
	if strings.Contains(out, "matchName: \"*.amazonaws.com\"") {
		t.Errorf("wildcard FQDN must not be a matchName:\n%s", out)
	}
}

// 6. Determinism: same input twice → byte-identical output.
func TestCilium_Determinism(t *testing.T) {
	build := func() string {
		ds := model.NewDependencySet("svc")
		ds.Add(model.NetworkDependency{Source: "svc", Target: "redis", Port: 6379, Protocol: "TCP"})
		ds.Add(model.NetworkDependency{Source: "svc", Target: "api.stripe.com", Port: 443, Protocol: "TCP"})
		ds.Add(model.NetworkDependency{Source: "svc", Target: "*.example.com", Port: 443, Protocol: "TCP"})
		return Cilium(ds)
	}
	a := build()
	b := build()
	if a != b {
		t.Errorf("non-deterministic Cilium output\nA:\n%s\nB:\n%s", a, b)
	}
}

// 7. Self-consistency: rendered output must NOT trigger MissingDNSEgress.
func TestCilium_SelfConsistency_NoMissingDNS(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{Source: "svc", Target: "api.stripe.com", Port: 443, Protocol: "TCP"})
	out := Cilium(ds)
	rep := parseAndValidate(t, out)
	for _, f := range rep.Findings {
		if f.Check == validator.CheckMissingDNSEgress {
			t.Errorf("rendered Cilium policy triggered MissingDNSEgress (segspec must not emit policies that fail its own validator):\n%s\nFinding: %+v", out, f)
		}
	}
}

// 8. Disabled-aware: deps with Disabled="egress" → no egress rules emitted.
func TestCilium_DisabledEgressSkipsRules(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{
		Source: "svc", Target: "postgres", Port: 5432, Protocol: "TCP",
		Disabled: "egress",
	})
	out := Cilium(ds)
	if strings.Contains(out, "app: postgres") {
		t.Errorf("Disabled=egress dep must not emit egress rule:\n%s", out)
	}
	if strings.Contains(out, "port: \"5432\"") {
		t.Errorf("Disabled=egress dep must not emit port rule:\n%s", out)
	}
}
