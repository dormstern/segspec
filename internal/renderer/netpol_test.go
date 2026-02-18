package renderer

import (
	"strings"
	"testing"

	"github.com/dormorgenstern/segspec/internal/model"
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
		"port: 53",   // DNS
		"protocol: UDP", // DNS UDP
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
