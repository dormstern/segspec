package validator_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/parser/netpol"
	"github.com/dormstern/segspec/internal/validator"
)

// parseString is a tiny helper that drops a YAML blob into a temp file and
// hands the parsed result back. Tests use it instead of inline-stuffing
// validator.Policy values because the parser path is itself part of the
// surface we're checking — every test exercises both layers.
func parseString(t *testing.T, name, body string) netpol.ParseResult {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	pr, err := netpol.ReadPath(p)
	if err != nil {
		t.Fatalf("ReadPath: %v", err)
	}
	return pr
}

// findCheck returns the first finding for the given check ID, or nil.
func findCheck(report validator.Report, id validator.CheckID) *validator.Finding {
	for i := range report.Findings {
		if report.Findings[i].Check == id {
			return &report.Findings[i]
		}
	}
	return nil
}

// Test 1 — happy path. A correctly-authored Cilium policy with toFQDNs
// AND a DNS-egress sibling rule should pass every check cleanly.
func TestRun_HappyPath_NoFindings(t *testing.T) {
	pr := parseString(t, "policy.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: web-allow-fqdn
  namespace: prod
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toFQDNs:
        - matchName: api.example.com
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            "k8s:io.kubernetes.pod.namespace": kube-system
            "k8s:k8s-app": kube-dns
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
`)
	rep := validator.Run(pr.Policies)
	if len(rep.Findings) != 0 {
		t.Fatalf("expected no findings on a clean policy, got %d: %+v", len(rep.Findings), rep.Findings)
	}
	if rep.PoliciesScanned != 1 {
		t.Errorf("expected PoliciesScanned=1, got %d", rep.PoliciesScanned)
	}
}

// Test 2 — MissingDNSEgress fires when toFQDNs is present but no port-53
// egress rule exists. Citation: cilium #44504.
func TestCheckMissingDNS_Triggers(t *testing.T) {
	pr := parseString(t, "fqdn-no-dns.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: web-fqdn-broken
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toFQDNs:
        - matchName: api.example.com
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
`)
	rep := validator.Run(pr.Policies)
	f := findCheck(rep, validator.CheckMissingDNSEgress)
	if f == nil {
		t.Fatalf("expected MissingDNSEgress finding, got %+v", rep.Findings)
	}
	if f.Severity != validator.SeverityError {
		t.Errorf("expected severity=error, got %s", f.Severity)
	}
	if f.Line == 0 {
		t.Errorf("expected nonzero evidence Line, got 0")
	}
	if !strings.Contains(f.Citation, "cilium/cilium/issues/44504") {
		t.Errorf("expected #44504 citation, got %q", f.Citation)
	}
}

// Test 3 — MissingDNSEgress does NOT fire when an explicit DNS egress
// rule (port 53) is present alongside toFQDNs.
func TestCheckMissingDNS_DoesNotTrigger_WithDNSPort(t *testing.T) {
	pr := parseString(t, "fqdn-with-dns.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: web-fqdn-ok
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toFQDNs:
        - matchName: api.example.com
    - toPorts:
        - ports:
            - port: "53"
              protocol: UDP
`)
	rep := validator.Run(pr.Policies)
	if f := findCheck(rep, validator.CheckMissingDNSEgress); f != nil {
		t.Fatalf("did not expect MissingDNSEgress when port 53 is present, got %+v", *f)
	}
}

// Test 4 — ToEntitiesWithToPorts fires when a Cilium egress rule combines
// both blocks. Citation: cilium #44504.
func TestCheckToEntitiesToPorts_Triggers(t *testing.T) {
	pr := parseString(t, "entities-and-ports.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: gotcha
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toEntities:
        - world
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
`)
	rep := validator.Run(pr.Policies)
	f := findCheck(rep, validator.CheckToEntitiesWithToPorts)
	if f == nil {
		t.Fatalf("expected ToEntitiesWithToPorts finding, got %+v", rep.Findings)
	}
	if f.Severity != validator.SeverityWarning {
		t.Errorf("expected severity=warning, got %s", f.Severity)
	}
	if !strings.Contains(f.Citation, "cilium/cilium/issues/44504") {
		t.Errorf("expected #44504 citation, got %q", f.Citation)
	}
}

// Test 5 — ToEntitiesWithToPorts does NOT fire when toEntities appears
// alone (port-less is the well-formed shape).
func TestCheckToEntitiesToPorts_DoesNotTrigger_WhenAlone(t *testing.T) {
	pr := parseString(t, "entities-only.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: entities-ok
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toEntities:
        - world
`)
	rep := validator.Run(pr.Policies)
	if f := findCheck(rep, validator.CheckToEntitiesWithToPorts); f != nil {
		t.Fatalf("did not expect ToEntitiesWithToPorts for bare toEntities, got %+v", *f)
	}
}

// Test 6 — OversizedSelectorLabel fires for a label value > 63 chars.
// Citation: cilium #43771.
func TestCheckOversizedLabels_Triggers(t *testing.T) {
	long := strings.Repeat("x", 70) // 70 > 63
	pr := parseString(t, "oversized.yaml", `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: oversized
spec:
  podSelector:
    matchLabels:
      app: `+long+`
`)
	rep := validator.Run(pr.Policies)
	f := findCheck(rep, validator.CheckOversizedSelectorLabel)
	if f == nil {
		t.Fatalf("expected OversizedSelectorLabel finding, got %+v", rep.Findings)
	}
	if f.Severity != validator.SeverityError {
		t.Errorf("expected severity=error, got %s", f.Severity)
	}
	if !strings.Contains(f.Citation, "cilium/cilium/issues/43771") {
		t.Errorf("expected #43771 citation, got %q", f.Citation)
	}
}

// Test 7 — OversizedSelectorLabel does NOT fire at exactly 63 chars
// (the limit is inclusive).
func TestCheckOversizedLabels_DoesNotTrigger_AtBoundary(t *testing.T) {
	atLimit := strings.Repeat("x", 63)
	pr := parseString(t, "boundary.yaml", `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: at-limit
spec:
  podSelector:
    matchLabels:
      app: `+atLimit+`
`)
	rep := validator.Run(pr.Policies)
	if f := findCheck(rep, validator.CheckOversizedSelectorLabel); f != nil {
		t.Fatalf("did not expect OversizedSelectorLabel at exactly 63 chars, got %+v", *f)
	}
}

// Test 8 — UnreferencedSelector fires when a policy's podSelector matches
// no workload in the input set.
func TestCheckUnreferenced_Triggers(t *testing.T) {
	pol := validator.Policy{
		Name:      "stale",
		File:      "pol.yaml",
		Line:      1,
		Namespace: "prod",
		PodSelector: []validator.LabelPair{
			{Key: "app", Value: "ghost", Line: 5},
		},
	}
	workloads := []validator.WorkloadLabels{
		{Namespace: "prod", Labels: map[string]string{"app": "web"}},
		{Namespace: "prod", Labels: map[string]string{"app": "api"}},
	}
	rep := validator.RunWithWorkloads([]validator.Policy{pol}, workloads)
	f := findCheck(rep, validator.CheckUnreferencedSelector)
	if f == nil {
		t.Fatalf("expected UnreferencedSelector finding, got %+v", rep.Findings)
	}
	if f.Severity != validator.SeverityWarning {
		t.Errorf("expected severity=warning, got %s", f.Severity)
	}
}

// Test 9 — UnreferencedSelector does NOT fire when a workload matches.
func TestCheckUnreferenced_DoesNotTrigger_WhenWorkloadMatches(t *testing.T) {
	pol := validator.Policy{
		Name:      "matched",
		File:      "pol.yaml",
		Line:      1,
		Namespace: "prod",
		PodSelector: []validator.LabelPair{
			{Key: "app", Value: "web", Line: 5},
		},
	}
	workloads := []validator.WorkloadLabels{
		{Namespace: "prod", Labels: map[string]string{"app": "web", "tier": "frontend"}},
	}
	rep := validator.RunWithWorkloads([]validator.Policy{pol}, workloads)
	if f := findCheck(rep, validator.CheckUnreferencedSelector); f != nil {
		t.Fatalf("did not expect UnreferencedSelector when a workload matches, got %+v", *f)
	}
}

// Test 10 — integration. A multi-file directory with one bad policy per
// failure mode plus a workload manifest yields one finding per check.
func TestRun_IntegrationDirectory(t *testing.T) {
	dir := t.TempDir()

	// Bad #1 — toFQDNs without DNS.
	mustWrite(t, dir, "fqdn-bad.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: fqdn-bad
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toFQDNs:
        - matchName: api.example.com
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
`)

	// Bad #2 — toEntities + toPorts.
	mustWrite(t, dir, "entities-bad.yaml", `
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: entities-bad
spec:
  endpointSelector:
    matchLabels:
      app: web
  egress:
    - toEntities: [world]
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
`)

	// Bad #3 — oversized label value.
	long := strings.Repeat("y", 80)
	mustWrite(t, dir, "label-bad.yaml", `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: label-bad
spec:
  podSelector:
    matchLabels:
      app: `+long+`
`)

	// Bad #4 — unreferenced selector. Plus a workload that matches a
	// DIFFERENT label so the unref check has ground truth.
	mustWrite(t, dir, "unref-bad.yaml", `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: unref-bad
  namespace: prod
spec:
  podSelector:
    matchLabels:
      app: ghost
`)
	mustWrite(t, dir, "workload.yaml", `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: real-web
  namespace: prod
spec:
  template:
    metadata:
      labels:
        app: web
`)

	pr, err := netpol.ReadPath(dir)
	if err != nil {
		t.Fatalf("ReadPath: %v", err)
	}
	rep := validator.RunWithWorkloads(pr.Policies, pr.Workloads)

	if rep.PoliciesScanned != 4 {
		t.Errorf("expected 4 policies scanned, got %d", rep.PoliciesScanned)
	}
	for _, want := range []validator.CheckID{
		validator.CheckMissingDNSEgress,
		validator.CheckToEntitiesWithToPorts,
		validator.CheckOversizedSelectorLabel,
		validator.CheckUnreferencedSelector,
	} {
		if findCheck(rep, want) == nil {
			t.Errorf("expected at least one %q finding, none in %+v", want, rep.Findings)
		}
	}
	if !rep.HasErrors() {
		t.Errorf("expected HasErrors() true (oversized + missing-dns are errors), got false")
	}
}

func mustWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
