package renderer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/explainer"
	"github.com/dormstern/segspec/internal/validator"
)

// TestExplainMarkdown_NoMatchingPolicies verifies the no-policy branch
// renders the explicit "allow-by-default" message rather than implying
// deny.
func TestExplainMarkdown_NoMatchingPolicies(t *testing.T) {
	exp := explainer.Explain(
		explainer.Workload{Name: "api", Namespace: "prod", Labels: map[string]string{"app": "api"}},
		nil,
	)
	md := ExplainMarkdown(exp)
	if !strings.Contains(md, "No NetworkPolicy in the input set selects this workload") {
		t.Errorf("expected 'no policy' message, got %q", md)
	}
	if !strings.Contains(md, "allow-by-default") {
		t.Errorf("expected explicit 'allow-by-default' message, got %q", md)
	}
}

// TestExplainMarkdown_SingleAllowPolicy verifies a single applied policy
// produces a contributed-rule line that includes the source file:line.
func TestExplainMarkdown_SingleAllowPolicy(t *testing.T) {
	pol := validator.Policy{
		Kind: "NetworkPolicy",
		Name: "api-allow",
		File: "policies/api.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api", Line: 5}},
		Ingress: []validator.IngressRule{{
			Line: 10,
			From: []validator.PeerSelector{{
				PodSelector: []validator.LabelPair{{Key: "app", Value: "web"}},
				Line:        12,
			}},
		}},
	}
	exp := explainer.Explain(
		explainer.Workload{Name: "api", Labels: map[string]string{"app": "api"}},
		[]validator.Policy{pol},
	)
	md := ExplainMarkdown(exp)
	if !strings.Contains(md, "api-allow") {
		t.Errorf("expected applied-policy name, got %q", md)
	}
	if !strings.Contains(md, "podSelector{app=web}") {
		t.Errorf("expected formatted peer in rule, got %q", md)
	}
	if !strings.Contains(md, "policies/api.yaml:10") {
		t.Errorf("expected file:line for ingress rule, got %q", md)
	}
	if !strings.Contains(md, "default-deny is in effect") {
		t.Errorf("expected default-deny note, got %q", md)
	}
}

// TestExplainMarkdown_DefaultDenyPlusTwoAllows verifies a workload selected
// by three policies (one default-deny shape, two allows) renders a unioned
// effective set with each rule traceable to its source.
func TestExplainMarkdown_DefaultDenyPlusTwoAllows(t *testing.T) {
	deny := validator.Policy{
		Kind: "NetworkPolicy", Name: "deny-all", File: "p/deny.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api"}},
		// Empty ingress with PodSelector matching: a real default-deny in
		// k8s sets policyTypes:Ingress with no rules. We model presence as
		// "has at least one ingress rule that allows nothing" — a peer-less
		// rule denies because its peer set is empty.
		// Here we model the presence implicitly by giving an Ingress entry
		// with no peers (matches "from anywhere"). The other two allow
		// policies still union in.
	}
	// The deny.yaml above declares no Ingress entries, so DefaultDenyIngress
	// won't flip from it alone — that's by design; the two allow policies
	// below trigger default-deny because they declare ingress.
	a1 := validator.Policy{
		Kind: "NetworkPolicy", Name: "allow-web", File: "p/web.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api"}},
		Ingress: []validator.IngressRule{{
			Line: 8,
			From: []validator.PeerSelector{{PodSelector: []validator.LabelPair{{Key: "app", Value: "web"}}}},
		}},
	}
	a2 := validator.Policy{
		Kind: "NetworkPolicy", Name: "allow-mon", File: "p/mon.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api"}},
		Ingress: []validator.IngressRule{{
			Line: 9,
			From: []validator.PeerSelector{{PodSelector: []validator.LabelPair{{Key: "app", Value: "monitor"}}}},
		}},
	}
	exp := explainer.Explain(
		explainer.Workload{Name: "api", Labels: map[string]string{"app": "api"}},
		[]validator.Policy{deny, a1, a2},
	)
	if len(exp.EffectiveIngress) != 2 {
		t.Fatalf("expected 2 effective ingress rules, got %d", len(exp.EffectiveIngress))
	}
	if !exp.DefaultDenyIngress {
		t.Errorf("expected default-deny ingress to be in effect")
	}
	md := ExplainMarkdown(exp)
	if !strings.Contains(md, "allow-web") || !strings.Contains(md, "allow-mon") {
		t.Errorf("expected both allow-policies named in output, got %q", md)
	}
	if !strings.Contains(md, "via `allow-web`") || !strings.Contains(md, "via `allow-mon`") {
		t.Errorf("expected effective-set rules to attribute the source policy, got %q", md)
	}
}

// TestExplainMarkdown_ConflictingLabelMatch verifies that policies which
// match via DIFFERENT label subsets all appear in the applied list (no
// dedup, no precedence — additive semantics).
func TestExplainMarkdown_ConflictingLabelMatch(t *testing.T) {
	byApp := validator.Policy{
		Kind: "NetworkPolicy", Name: "by-app", File: "p/byapp.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api"}},
		Ingress:     []validator.IngressRule{{Line: 5}},
	}
	byTier := validator.Policy{
		Kind: "NetworkPolicy", Name: "by-tier", File: "p/bytier.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "tier", Value: "backend"}},
		Ingress:     []validator.IngressRule{{Line: 6}},
	}
	byBoth := validator.Policy{
		Kind: "NetworkPolicy", Name: "by-both", File: "p/byboth.yaml", Line: 1,
		PodSelector: []validator.LabelPair{
			{Key: "app", Value: "api"},
			{Key: "tier", Value: "backend"},
		},
		Ingress: []validator.IngressRule{{Line: 7}},
	}
	notMatch := validator.Policy{
		Kind: "NetworkPolicy", Name: "by-other", File: "p/byo.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "frontend"}},
		Ingress:     []validator.IngressRule{{Line: 8}},
	}
	exp := explainer.Explain(
		explainer.Workload{Name: "api", Labels: map[string]string{"app": "api", "tier": "backend"}},
		[]validator.Policy{byApp, byTier, byBoth, notMatch},
	)
	if len(exp.Policies) != 3 {
		t.Fatalf("expected 3 applied policies, got %d (%v)", len(exp.Policies), exp.Policies)
	}
	md := ExplainMarkdown(exp)
	for _, want := range []string{"by-app", "by-tier", "by-both"} {
		if !strings.Contains(md, want) {
			t.Errorf("expected policy %q listed, got %q", want, md)
		}
	}
	if strings.Contains(md, "by-other") {
		t.Errorf("by-other should NOT match; output: %q", md)
	}
}

// TestExplainJSON_Structure verifies the structured contract: the JSON
// output has the documented top-level keys and is parseable.
func TestExplainJSON_Structure(t *testing.T) {
	pol := validator.Policy{
		Kind: "NetworkPolicy", Name: "api-allow",
		File: "policies/api.yaml", Line: 1,
		PodSelector: []validator.LabelPair{{Key: "app", Value: "api"}},
		Egress: []validator.EgressRule{{
			Line:         12,
			ToPortsPorts: []int{443},
		}},
	}
	exp := explainer.Explain(
		explainer.Workload{Name: "api", Namespace: "prod", Labels: map[string]string{"app": "api"}},
		[]validator.Policy{pol},
	)
	body, err := ExplainJSON(exp)
	if err != nil {
		t.Fatalf("ExplainJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(body))
	}
	for _, k := range []string{"workload", "policies", "effective_ingress", "effective_egress", "default_deny_ingress", "default_deny_egress"} {
		if _, ok := got[k]; !ok {
			t.Errorf("expected top-level key %q in JSON, missing. got: %v", k, got)
		}
	}
	w, ok := got["workload"].(map[string]any)
	if !ok {
		t.Fatalf("expected workload object, got %T", got["workload"])
	}
	if w["name"] != "api" {
		t.Errorf("expected workload.name=api, got %v", w["name"])
	}
	pols, ok := got["policies"].([]any)
	if !ok || len(pols) != 1 {
		t.Fatalf("expected 1 policy in JSON, got %v", got["policies"])
	}
	if got["default_deny_egress"] != true {
		t.Errorf("expected default_deny_egress=true, got %v", got["default_deny_egress"])
	}
}
