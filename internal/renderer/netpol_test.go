package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestNetworkPolicyBasic(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Confidence: model.High,
	})

	out := NetworkPolicy(ds)

	checks := []string{
		"apiVersion: networking.k8s.io/v1",
		"kind: NetworkPolicy",
		"name: order-service-egress",
		"generated-by: segspec",
		"app: order-service",
		"Egress",
		"port: 5432",
		"protocol: TCP",
		"port: 53",      // DNS
		"protocol: UDP",  // DNS UDP
		"app: postgres",  // Fix 1: proper destination selector
	}

	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing %q", check)
		}
	}
}

func TestNetworkPolicyEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty")
	out := NetworkPolicy(ds)
	if out != "" {
		t.Errorf("expected empty output for no deps, got %q", out)
	}
}

func TestNetworkPolicyMultiplePorts(t *testing.T) {
	ds := model.NewDependencySet("multi-app")
	ds.Add(model.NetworkDependency{Source: "multi-app", Target: "postgres", Port: 5432, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "multi-app", Target: "redis", Port: 6379, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "multi-app", Target: "kafka", Port: 9092, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	if !strings.Contains(out, "port: 5432") {
		t.Error("missing postgres port")
	}
	if !strings.Contains(out, "port: 6379") {
		t.Error("missing redis port")
	}
	if !strings.Contains(out, "port: 9092") {
		t.Error("missing kafka port")
	}
}

func TestNetworkPolicySanitizesName(t *testing.T) {
	ds := model.NewDependencySet("My App_v2.0")
	ds.Add(model.NetworkDependency{Source: "x", Target: "db", Port: 5432, Protocol: "TCP"})

	out := NetworkPolicy(ds)
	if !strings.Contains(out, "name: my-app-v2-0-egress") {
		t.Errorf("name not sanitized correctly in output:\n%s", out)
	}
}

func TestNetworkPolicyDNSAlwaysPresent(t *testing.T) {
	ds := model.NewDependencySet("app")
	ds.Add(model.NetworkDependency{Source: "app", Target: "db", Port: 3306, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// DNS should always be in egress rules
	if strings.Count(out, "port: 53") != 2 { // UDP + TCP
		t.Error("DNS egress (port 53 UDP+TCP) should always be present")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"order-service", "order-service"},
		{"My App", "my-app"},
		{"app_v2.0", "app-v2-0"},
		{"---leading---", "leading"},
		{"UPPERCASE", "uppercase"},
		// Fix 3: edge cases
		{"___", "unknown"},
		{"...", "unknown"},
		{"", "unknown"},
		{strings.Repeat("a", 100), strings.Repeat("a", 63)},
		// Trailing hyphens after truncation: 62 a's + "-b" = 64 chars, truncated to 63 = 62 a's + "-", then trim trailing hyphen
		{strings.Repeat("a", 62) + "-b", strings.Repeat("a", 62)},
		{strings.Repeat("a", 62) + "--", strings.Repeat("a", 62)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Fix 1: Egress rules must have destination selectors
func TestNetworkPolicyEgressDestinationSelectors(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// Should have a podSelector for the target, not just a comment
	if !strings.Contains(out, "podSelector:") {
		t.Error("egress rule should have podSelector for simple service target")
	}
	if !strings.Contains(out, "app: postgres") {
		t.Errorf("egress rule should have matchLabels app: postgres, got:\n%s", out)
	}
	// Should have the review comment
	if !strings.Contains(out, "# Review: verify podSelector labels match your deployment") {
		t.Error("missing review comment above generated policy")
	}
}

func TestNetworkPolicyEgressIPTarget(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "10.0.1.5", Port: 443, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	if !strings.Contains(out, "ipBlock:") {
		t.Error("IP target should use ipBlock")
	}
	if !strings.Contains(out, "cidr: 10.0.1.5/32") {
		t.Errorf("IP target should have cidr with /32, got:\n%s", out)
	}
}

func TestNetworkPolicyEgressFQDNTarget(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "mydb.production.svc.cluster.local", Port: 5432, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	if !strings.Contains(out, "podSelector:") {
		t.Error("FQDN target should use podSelector")
	}
	if !strings.Contains(out, "app: mydb") {
		t.Errorf("FQDN target should extract service name, got:\n%s", out)
	}
	if !strings.Contains(out, "namespaceSelector:") {
		t.Error("FQDN target should use namespaceSelector")
	}
	if !strings.Contains(out, "kubernetes.io/metadata.name: production") {
		t.Errorf("FQDN target should extract namespace, got:\n%s", out)
	}
}

func TestNetworkPolicyDNSRestricted(t *testing.T) {
	ds := model.NewDependencySet("app")
	ds.Add(model.NetworkDependency{Source: "app", Target: "db", Port: 3306, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// DNS egress should be restricted to kube-system namespace
	if !strings.Contains(out, "kubernetes.io/metadata.name: kube-system") {
		t.Error("DNS egress should be restricted to kube-system namespace")
	}
}

// Fix 2: Port 0 should be skipped
func TestNetworkPolicySkipsPortZero(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "unknown-svc", Port: 0, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "web", Target: "redis", Port: 6379, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	if strings.Contains(out, "port: 0") {
		t.Error("port 0 should not appear in output")
	}
	if !strings.Contains(out, "port: 6379") {
		t.Error("valid port 6379 should still be present")
	}
	if !strings.Contains(out, "# Skipped (no port): unknown-svc") {
		t.Errorf("should have a comment listing skipped deps, got:\n%s", out)
	}
}

func TestNetworkPolicySkipsNegativePort(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "bad-svc", Port: -1, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "web", Target: "redis", Port: 6379, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	if strings.Contains(out, "port: -1") {
		t.Error("negative port should not appear in output")
	}
	if !strings.Contains(out, "# Skipped (no port): bad-svc") {
		t.Errorf("should list skipped dep, got:\n%s", out)
	}
}

func TestNetworkPolicyAllDepsSkipped(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "svc-a", Port: 0, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "web", Target: "svc-b", Port: 0, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// Should still produce output (default deny + DNS) even if all deps skipped
	if out == "" {
		t.Error("should produce output even when all deps are skipped (default deny + DNS)")
	}
	if !strings.Contains(out, "# Skipped (no port): svc-a, svc-b") {
		t.Errorf("should list all skipped deps, got:\n%s", out)
	}
}

// Fix 4: Default deny + ingress policies
func TestNetworkPolicyDefaultDeny(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "db", Port: 5432, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// Should contain a default-deny policy
	if !strings.Contains(out, "# Default deny policy") {
		t.Error("should contain a default deny policy comment")
	}
	// The default deny policy should block both ingress and egress
	if !strings.Contains(out, "- Ingress") {
		t.Error("default deny should include Ingress policyType")
	}
	if !strings.Contains(out, "- Egress") {
		t.Error("default deny should include Egress policyType")
	}
}

func TestNetworkPolicyTwoPolicies(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{Source: "web", Target: "db", Port: 5432, Protocol: "TCP"})

	out := NetworkPolicy(ds)

	// Should contain two separate NetworkPolicy documents (separated by ---)
	count := strings.Count(out, "kind: NetworkPolicy")
	if count != 2 {
		t.Errorf("expected 2 NetworkPolicy documents, got %d:\n%s", count, out)
	}
	// Should have YAML document separator
	if !strings.Contains(out, "---") {
		t.Error("multiple policies should be separated by ---")
	}
}

func TestPerServiceNetworkPolicy(t *testing.T) {
	ds := model.NewDependencySet("myapp")
	ds.Add(model.NetworkDependency{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP"})

	output := PerServiceNetworkPolicy(ds)

	// Should contain policies for frontend, cartservice, and redis
	if !strings.Contains(output, "name: frontend-netpol") {
		t.Error("missing frontend policy")
	}
	if !strings.Contains(output, "name: cartservice-netpol") {
		t.Error("missing cartservice policy")
	}
	if !strings.Contains(output, "name: redis-netpol") {
		t.Error("missing redis policy")
	}

	// cartservice should have ingress from frontend
	if !strings.Contains(output, "app: frontend") {
		t.Error("missing ingress from frontend in cartservice policy")
	}

	// cartservice should have egress to redis
	if !strings.Contains(output, "app: redis") {
		t.Error("missing egress to redis in cartservice policy")
	}

	// All services should have Ingress and Egress in policyTypes
	if strings.Count(output, "- Ingress") < 3 {
		t.Errorf("expected Ingress policyType in each service policy, got %d", strings.Count(output, "- Ingress"))
	}
	if strings.Count(output, "- Egress") < 3 {
		t.Errorf("expected Egress policyType in each service policy, got %d", strings.Count(output, "- Egress"))
	}
}

func TestPerServiceNetworkPolicyEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty")
	output := PerServiceNetworkPolicy(ds)
	if output != "" {
		t.Errorf("expected empty output for no deps, got %q", output)
	}
}

func TestPerServiceNetworkPolicyDNSEgress(t *testing.T) {
	ds := model.NewDependencySet("myapp")
	ds.Add(model.NetworkDependency{Source: "frontend", Target: "api", Port: 8080, Protocol: "TCP"})

	output := PerServiceNetworkPolicy(ds)

	// frontend has egress, so it should get DNS egress rule
	if !strings.Contains(output, "kube-system") {
		t.Error("missing DNS egress to kube-system for service with egress rules")
	}
}

func TestPerServiceNetworkPolicySkipsInvalidPort(t *testing.T) {
	ds := model.NewDependencySet("myapp")
	ds.Add(model.NetworkDependency{Source: "frontend", Target: "api", Port: 8080, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "frontend", Target: "unknown", Port: 0, Protocol: "TCP"})

	output := PerServiceNetworkPolicy(ds)

	if strings.Contains(output, "port: 0") {
		t.Error("port 0 should not appear in output")
	}
	if !strings.Contains(output, "port: 8080") {
		t.Error("valid port 8080 should be present")
	}
}
