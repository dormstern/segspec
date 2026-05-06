package renderer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dormstern/segspec/internal/explainer"
)

// ExplainMarkdown renders a human-readable Markdown report explaining
// which policies apply to a workload, what each contributes, and what the
// effective allow-set is once they're unioned.
//
// The output format is intentionally not a contract — it's a debugging
// aid. JSON is the structured contract (see ExplainJSON).
func ExplainMarkdown(exp explainer.Explanation) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# NetworkPolicy explanation: `%s`\n\n", exp.Workload.Name)
	if exp.Workload.Namespace != "" {
		fmt.Fprintf(&b, "Namespace: `%s`\n\n", exp.Workload.Namespace)
	}

	if len(exp.Workload.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: ")
		first := true
		// Deterministic label rendering.
		keys := sortedKeys(exp.Workload.Labels)
		for _, k := range keys {
			if !first {
				fmt.Fprintf(&b, ", ")
			}
			first = false
			fmt.Fprintf(&b, "`%s=%s`", k, exp.Workload.Labels[k])
		}
		fmt.Fprintf(&b, "\n\n")
	}

	// --- No policies branch ----------------------------------------------
	if len(exp.Policies) == 0 {
		fmt.Fprintf(&b, "## Result\n\n")
		fmt.Fprintf(&b, "_No NetworkPolicy in the input set selects this workload._\n\n")
		fmt.Fprintf(&b, "Effective behavior: **allow-by-default** for both ingress and egress (Kubernetes' baseline). ")
		fmt.Fprintf(&b, "Any pod in the cluster may reach this workload, and this workload may reach anywhere — until at least one policy is added that selects it.\n")
		return b.String()
	}

	// --- Applied policies section ----------------------------------------
	fmt.Fprintf(&b, "## Applied policies (%d)\n\n", len(exp.Policies))
	for _, ap := range exp.Policies {
		fmt.Fprintf(&b, "### `%s`", ap.Name)
		if ap.Namespace != "" {
			fmt.Fprintf(&b, " (ns: `%s`)", ap.Namespace)
		}
		fmt.Fprintf(&b, "\n\n")
		fmt.Fprintf(&b, "Source: `%s:%d` — kind: `%s`\n\n", ap.File, ap.Line, ap.Kind)

		if len(ap.IngressRules) > 0 {
			fmt.Fprintf(&b, "**Ingress rules contributed:**\n\n")
			for _, r := range ap.IngressRules {
				fmt.Fprintf(&b, "- %s _(`%s:%d`)_\n", r.Description, r.File, r.Line)
			}
			fmt.Fprintf(&b, "\n")
		}
		if len(ap.EgressRules) > 0 {
			fmt.Fprintf(&b, "**Egress rules contributed:**\n\n")
			for _, r := range ap.EgressRules {
				fmt.Fprintf(&b, "- %s _(`%s:%d`)_\n", r.Description, r.File, r.Line)
			}
			fmt.Fprintf(&b, "\n")
		}
		if len(ap.IngressRules) == 0 && len(ap.EgressRules) == 0 {
			fmt.Fprintf(&b, "_This policy selects the workload but declares no allow rules — it contributes default-deny only._\n\n")
		}
	}

	// --- Effective allow-set ---------------------------------------------
	fmt.Fprintf(&b, "## Effective allow-set\n\n")

	fmt.Fprintf(&b, "**Ingress** — ")
	if exp.DefaultDenyIngress {
		fmt.Fprintf(&b, "default-deny is in effect (at least one policy declares ingress rules).\n\n")
	} else {
		fmt.Fprintf(&b, "no policy declares ingress; allow-by-default applies.\n\n")
	}
	if len(exp.EffectiveIngress) == 0 {
		if exp.DefaultDenyIngress {
			fmt.Fprintf(&b, "_No ingress is allowed. All inbound traffic is denied._\n\n")
		}
	} else {
		for _, r := range exp.EffectiveIngress {
			fmt.Fprintf(&b, "- %s _(via `%s` at `%s:%d`)_\n", r.Description, r.PolicyName, r.File, r.Line)
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "**Egress** — ")
	if exp.DefaultDenyEgress {
		fmt.Fprintf(&b, "default-deny is in effect (at least one policy declares egress rules).\n\n")
	} else {
		fmt.Fprintf(&b, "no policy declares egress; allow-by-default applies.\n\n")
	}
	if len(exp.EffectiveEgress) == 0 {
		if exp.DefaultDenyEgress {
			fmt.Fprintf(&b, "_No egress is allowed. All outbound traffic is denied._\n\n")
		}
	} else {
		for _, r := range exp.EffectiveEgress {
			fmt.Fprintf(&b, "- %s _(via `%s` at `%s:%d`)_\n", r.Description, r.PolicyName, r.File, r.Line)
		}
		fmt.Fprintf(&b, "\n")
	}

	return b.String()
}

// explainJSONShape is the wire format for ExplainJSON. Stable across
// versions — adding fields is fine, renaming or removing is breaking.
type explainJSONShape struct {
	Workload struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace,omitempty"`
		Labels    map[string]string `json:"labels,omitempty"`
	} `json:"workload"`

	Policies []explainPolicyShape `json:"policies"`

	EffectiveIngress []explainRuleShape `json:"effective_ingress"`
	EffectiveEgress  []explainRuleShape `json:"effective_egress"`

	DefaultDenyIngress bool `json:"default_deny_ingress"`
	DefaultDenyEgress  bool `json:"default_deny_egress"`
}

type explainPolicyShape struct {
	Name      string             `json:"name"`
	Namespace string             `json:"namespace,omitempty"`
	File      string             `json:"file"`
	Line      int                `json:"line"`
	Kind      string             `json:"kind"`
	Ingress   []explainRuleShape `json:"ingress_rules"`
	Egress    []explainRuleShape `json:"egress_rules"`
}

type explainRuleShape struct {
	Description string `json:"description"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	PolicyName  string `json:"policy_name"`
}

// ExplainJSON renders the explanation as deterministic JSON. The schema is
// the structured contract for `segspec explain --json` consumers.
func ExplainJSON(exp explainer.Explanation) ([]byte, error) {
	var out explainJSONShape
	out.Workload.Name = exp.Workload.Name
	out.Workload.Namespace = exp.Workload.Namespace
	if len(exp.Workload.Labels) > 0 {
		out.Workload.Labels = exp.Workload.Labels
	}

	out.Policies = make([]explainPolicyShape, 0, len(exp.Policies))
	for _, ap := range exp.Policies {
		ps := explainPolicyShape{
			Name:      ap.Name,
			Namespace: ap.Namespace,
			File:      ap.File,
			Line:      ap.Line,
			Kind:      ap.Kind,
			Ingress:   convertRules(ap.IngressRules),
			Egress:    convertRules(ap.EgressRules),
		}
		out.Policies = append(out.Policies, ps)
	}
	out.EffectiveIngress = convertRules(exp.EffectiveIngress)
	out.EffectiveEgress = convertRules(exp.EffectiveEgress)
	out.DefaultDenyIngress = exp.DefaultDenyIngress
	out.DefaultDenyEgress = exp.DefaultDenyEgress

	return json.MarshalIndent(out, "", "  ")
}

func convertRules(rules []explainer.RuleSummary) []explainRuleShape {
	if len(rules) == 0 {
		return []explainRuleShape{}
	}
	out := make([]explainRuleShape, 0, len(rules))
	for _, r := range rules {
		out = append(out, explainRuleShape{
			Description: r.Description,
			File:        r.File,
			Line:        r.Line,
			PolicyName:  r.PolicyName,
		})
	}
	return out
}

// sortedKeys returns the deterministic key list of a map[string]string.
// Local helper so we don't pull in the `sort` package twice.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Tiny insertion sort — n is the label count, typically <10.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
