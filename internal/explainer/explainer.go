// Package explainer answers the question:
// "Given this workload, which NetworkPolicies apply to it and what traffic
// do they allow / deny?"
//
// The wedge: K8s NetworkPolicy is additive — multiple policies that select
// the same workload union their allow rules, and the presence of ANY policy
// flips that workload from allow-by-default to deny-by-default for the
// affected direction (ingress / egress). Operators get this wrong constantly
// because there is no built-in `kubectl explain-policy <pod>` and the
// behavior is non-local: a policy in another file, with a selector that
// happens to match, silently changes the effective allow-set.
//
// This package is a pure function over the validator.Policy projection
// produced by internal/parser/netpol — it never reads YAML directly, never
// touches the cluster, and emits a deterministic Explanation that the
// renderer turns into Markdown or JSON.
//
// Free tier — this is a debugging aid, not a paid feature.
package explainer

import (
	"sort"

	"github.com/dormstern/segspec/internal/validator"
)

// Workload is the input to Explain — a single named pod / deployment we
// want to reason about. The Labels map is matched against every policy's
// PodSelector using K8s matchLabels semantics (every required label must
// be present and equal).
type Workload struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

// AppliedPolicy records one NetworkPolicy that selects the workload, plus
// the rules it contributes. Kept as a flat shape so the renderer doesn't
// have to reach back into validator types.
type AppliedPolicy struct {
	Name      string
	Namespace string
	File      string
	Line      int
	Kind      string // "NetworkPolicy" or "CiliumNetworkPolicy" / "CiliumClusterwideNetworkPolicy"

	// IngressRules / EgressRules carry the human-readable form of each rule
	// that this policy contributes to the workload's effective allow-set.
	IngressRules []RuleSummary
	EgressRules  []RuleSummary
}

// RuleSummary is one allow line in the effective set, traceable back to
// its source policy file:line. The Description is rendered verbatim into
// the Markdown / JSON output.
type RuleSummary struct {
	Description string
	File        string
	Line        int
	PolicyName  string
}

// Explanation is the full answer to "what traffic does this workload
// allow / deny, and why". The renderer consumes this directly.
type Explanation struct {
	Workload Workload

	// Policies is every policy that selected the workload, in deterministic
	// order (namespace, name, file).
	Policies []AppliedPolicy

	// EffectiveIngress / EffectiveEgress are the union of allow-rules across
	// every applied policy. When no policy applies to the workload at all,
	// both slices are empty AND DefaultDenyIngress / DefaultDenyEgress are
	// false — meaning the cluster's allow-by-default applies. When ANY
	// policy applies for a direction, the corresponding default-deny flag
	// flips to true and the effective set is the (possibly empty) union.
	EffectiveIngress []RuleSummary
	EffectiveEgress  []RuleSummary

	DefaultDenyIngress bool
	DefaultDenyEgress  bool
}

// Explain computes the effective allow-set for the given workload across
// the input set of policies. Pure function — deterministic for a given
// (workload, policies) pair.
func Explain(w Workload, policies []validator.Policy) Explanation {
	exp := Explanation{Workload: w}

	for _, p := range policies {
		// Namespace match: a policy in another namespace cannot select this
		// workload. Cilium ClusterwideNetworkPolicy lives in no namespace —
		// treated as cluster-scoped, matches anywhere.
		if p.Namespace != "" && w.Namespace != "" && p.Namespace != w.Namespace {
			continue
		}
		if !labelsMatch(p.PodSelector, w.Labels) {
			continue
		}

		ap := AppliedPolicy{
			Name:      p.Name,
			Namespace: p.Namespace,
			File:      p.File,
			Line:      p.Line,
			Kind:      p.Kind,
		}

		for _, ing := range p.Ingress {
			ap.IngressRules = append(ap.IngressRules, summarizeIngress(p, ing))
		}
		for _, eg := range p.Egress {
			ap.EgressRules = append(ap.EgressRules, summarizeEgress(p, eg))
		}

		// A policy that selects the workload turns on default-deny for the
		// directions it declares — even if that direction is empty, the
		// presence alone flips the default.
		if len(p.Ingress) > 0 {
			exp.DefaultDenyIngress = true
		}
		if len(p.Egress) > 0 {
			exp.DefaultDenyEgress = true
		}

		exp.Policies = append(exp.Policies, ap)
	}

	// Sort policies for deterministic output.
	sort.SliceStable(exp.Policies, func(i, j int) bool {
		a, b := exp.Policies[i], exp.Policies[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.File < b.File
	})

	// Build the effective union — order = source-policy order = stable.
	for _, ap := range exp.Policies {
		exp.EffectiveIngress = append(exp.EffectiveIngress, ap.IngressRules...)
		exp.EffectiveEgress = append(exp.EffectiveEgress, ap.EgressRules...)
	}

	return exp
}

// labelsMatch implements K8s matchLabels semantics: every required pair
// must be present with an equal value in the workload's labels. An empty
// selector ("select all") returns true.
func labelsMatch(required []validator.LabelPair, actual map[string]string) bool {
	for _, lp := range required {
		v, ok := actual[lp.Key]
		if !ok || v != lp.Value {
			return false
		}
	}
	return true
}

// summarizeIngress turns a parsed ingress rule into a one-line description
// suitable for the Markdown / JSON output.
func summarizeIngress(p validator.Policy, ing validator.IngressRule) RuleSummary {
	desc := "ingress: "
	if len(ing.From) == 0 {
		desc += "from anywhere (no peer restriction)"
	} else {
		peers := make([]string, 0, len(ing.From))
		for _, peer := range ing.From {
			peers = append(peers, formatPeer(peer))
		}
		desc += "from " + joinHumane(peers)
	}
	return RuleSummary{
		Description: desc,
		File:        p.File,
		Line:        nonZeroLine(ing.Line, p.Line),
		PolicyName:  p.Name,
	}
}

// summarizeEgress turns a parsed egress rule into a one-line description.
func summarizeEgress(p validator.Policy, eg validator.EgressRule) RuleSummary {
	desc := "egress: "
	parts := []string{}

	if len(eg.To) > 0 {
		peers := make([]string, 0, len(eg.To))
		for _, peer := range eg.To {
			peers = append(peers, formatPeer(peer))
		}
		parts = append(parts, "to "+joinHumane(peers))
	}
	if len(eg.ToFQDNs) > 0 {
		fqdns := make([]string, 0, len(eg.ToFQDNs))
		for _, f := range eg.ToFQDNs {
			if f.MatchName != "" {
				fqdns = append(fqdns, f.MatchName)
			} else if f.MatchPattern != "" {
				fqdns = append(fqdns, f.MatchPattern)
			}
		}
		if len(fqdns) > 0 {
			parts = append(parts, "to FQDNs "+joinHumane(fqdns))
		}
	}
	if len(eg.ToEntities) > 0 {
		parts = append(parts, "to entities "+joinHumane(eg.ToEntities))
	}
	if len(eg.ToPortsPorts) > 0 {
		ports := make([]string, 0, len(eg.ToPortsPorts))
		for _, n := range eg.ToPortsPorts {
			ports = append(ports, intStr(n))
		}
		parts = append(parts, "on ports "+joinHumane(ports))
	}
	if len(parts) == 0 {
		desc += "to anywhere (no peer restriction)"
	} else {
		desc += joinParts(parts)
	}
	return RuleSummary{
		Description: desc,
		File:        p.File,
		Line:        nonZeroLine(eg.Line, p.Line),
		PolicyName:  p.Name,
	}
}

// formatPeer renders a podSelector peer as "podSelector{key=val,...}". An
// empty peer is rendered as "<all-pods-in-namespace>".
func formatPeer(peer validator.PeerSelector) string {
	if len(peer.PodSelector) == 0 {
		return "<all-pods-in-namespace>"
	}
	pairs := make([]string, 0, len(peer.PodSelector))
	for _, lp := range peer.PodSelector {
		pairs = append(pairs, lp.Key+"="+lp.Value)
	}
	return "podSelector{" + joinHumane(pairs) + "}"
}

// joinHumane is a tiny csv helper. Avoids pulling in strings.Join repeatedly
// to keep dependencies low, and keeps formatting consistent across calls.
func joinHumane(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// joinParts joins egress segments with " and " — "to x and on ports 80".
func joinParts(parts []string) string {
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += " and "
		}
		out += s
	}
	return out
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func nonZeroLine(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}
