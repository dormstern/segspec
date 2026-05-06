package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

// TestSummary_DisabledAnnotation verifies that the human-readable summary
// surfaces a `[disabled: <value>]` marker so operators can SEE that a
// workload's policies are intentionally skipped (k8s #112560 — "disable
// temporarily without deleting").
func TestSummary_DisabledAnnotation(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Description: "PostgreSQL",
		Confidence: model.High, SourceFile: "application.yml",
		Disabled: "ingress",
	})

	out := Summary(ds)

	if !strings.Contains(out, "[disabled: ingress]") {
		t.Errorf("summary missing [disabled: ingress] marker:\n%s", out)
	}
}

// TestPerServiceNetpol_SkipsFullyDisabled verifies that a workload with
// Disabled="full" emits NO NetworkPolicy at all (the whole point of the
// directive — the policy disappears, the comment stays).
func TestPerServiceNetpol_SkipsFullyDisabled(t *testing.T) {
	ds := model.NewDependencySet("graph")
	// disabled workload: web (full)
	ds.Add(model.NetworkDependency{
		Source: "web", Target: "db", Port: 5432, Protocol: "TCP",
		Confidence: model.High, Disabled: "full",
	})
	// enabled sibling: api → cache
	ds.Add(model.NetworkDependency{
		Source: "api", Target: "cache", Port: 6379, Protocol: "TCP",
		Confidence: model.High,
	})

	out := PerServiceNetworkPolicy(ds)

	// The disabled workload's NetworkPolicy must not appear.
	if strings.Contains(out, "name: web-netpol") {
		t.Errorf("expected web-netpol to be skipped (Disabled=full), got:\n%s", out)
	}
	// The enabled sibling must still render.
	if !strings.Contains(out, "name: api-netpol") {
		t.Errorf("expected api-netpol to render, got:\n%s", out)
	}
}
