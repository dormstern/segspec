package formats

import "testing"

// Each canonical --format value must pass through Canonicalize
// unchanged with wasAlias=false. If a canonical name silently became
// "wasAlias=true" it would emit a spurious deprecation warning every
// run; if it got rewritten the format switch in cmd/analyze.go would
// stop matching. This is the load-bearing behavior the rest of the
// CLI depends on.
func TestCanonicalize_CanonicalNamesPassThrough(t *testing.T) {
	canonical := []string{
		"summary", "netpol", "per-service", "default-deny", "all",
		"evidence", "audit", "json", "evidence-bundle",
		"evidence-bundle-sarif", "cilium",
	}
	for _, name := range canonical {
		got, wasAlias := Canonicalize(name)
		if got != name {
			t.Errorf("Canonicalize(%q) = %q, want %q (canonical names must not rewrite)", name, got, name)
		}
		if wasAlias {
			t.Errorf("Canonicalize(%q) wasAlias=true, want false (canonical names must not trigger deprecation)", name)
		}
	}
}

// Every documented alias must resolve to its canonical form and
// signal wasAlias=true so the dispatch can emit a stderr warning.
// The 8 mappings encode the spec — losing any of them silently turns
// "guess the spelling" back into an "unknown format" error.
func TestCanonicalize_KnownAliasesResolve(t *testing.T) {
	cases := map[string]string{
		"networkpolicy":         "netpol",
		"network-policy":        "netpol",
		"audit-ledger":          "audit",
		"default-deny-only":     "default-deny",
		"evidencebundle":        "evidence-bundle",
		"evidence_bundle":       "evidence-bundle",
		"cilium-network-policy": "cilium",
		"cnp":                   "cilium",
	}
	for alias, want := range cases {
		got, wasAlias := Canonicalize(alias)
		if got != want {
			t.Errorf("Canonicalize(%q) = %q, want %q", alias, got, want)
		}
		if !wasAlias {
			t.Errorf("Canonicalize(%q) wasAlias=false, want true (alias must signal deprecation)", alias)
		}
	}
}

// Unknown formats must pass through unchanged so the caller's own
// "unknown format: X" branch keeps producing the user-facing error.
// Rewriting an unknown value here would mask typos.
func TestCanonicalize_UnknownPassesThrough(t *testing.T) {
	for _, garbage := range []string{"banana", "", "yamlpolicy", "evidence-bundle-xml"} {
		got, wasAlias := Canonicalize(garbage)
		if got != garbage {
			t.Errorf("Canonicalize(%q) = %q, want unchanged", garbage, got)
		}
		if wasAlias {
			t.Errorf("Canonicalize(%q) wasAlias=true, want false (unknown must not pose as alias)", garbage)
		}
	}
}

// Case-insensitive matching: operators copy-paste from docs and
// shells with mixed casing. The lookup must hit on NETWORKPOLICY,
// NetworkPolicy, and networkpolicy alike.
func TestCanonicalize_CaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"NETWORKPOLICY":         "netpol",
		"NetworkPolicy":         "netpol",
		"Network-Policy":        "netpol",
		"CNP":                   "cilium",
		"Cilium-Network-Policy": "cilium",
		"Evidence_Bundle":       "evidence-bundle",
	}
	for alias, want := range cases {
		got, wasAlias := Canonicalize(alias)
		if got != want {
			t.Errorf("Canonicalize(%q) = %q, want %q", alias, got, want)
		}
		if !wasAlias {
			t.Errorf("Canonicalize(%q) wasAlias=false, want true", alias)
		}
	}
}

// Idempotence: feeding Canonicalize's own output back in must yield
// the same value with wasAlias=false. Otherwise alias-of-alias chains
// would force callers to loop, which the dispatch in cmd/analyze.go
// is not built to do.
func TestCanonicalize_Idempotent(t *testing.T) {
	for _, alias := range []string{
		"networkpolicy", "network-policy", "audit-ledger",
		"default-deny-only", "evidencebundle", "evidence_bundle",
		"cilium-network-policy", "cnp",
	} {
		first, _ := Canonicalize(alias)
		second, wasAlias := Canonicalize(first)
		if second != first {
			t.Errorf("Canonicalize(%q)=%q then Canonicalize(%q)=%q — not idempotent", alias, first, first, second)
		}
		if wasAlias {
			t.Errorf("re-canonicalizing %q (canonical) returned wasAlias=true; chains forbidden", first)
		}
	}
}
