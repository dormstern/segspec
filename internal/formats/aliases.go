// Package formats centralizes --format value handling for segspec
// commands. The package today exposes a single concern: alias
// canonicalization. Operators routinely guess at format names
// (`networkpolicy`, `cilium-network-policy`, `evidence_bundle`) and
// each near-miss surfaces as a confusing "unknown format" error. By
// recognizing the alias and emitting a stderr deprecation warning
// the CLI stays forgiving without locking in a second spelling as a
// permanent contract.
//
// The framework is intentionally small: a single map, a single
// lookup. Future format renames hook in by adding entries here; call
// sites only need to call Canonicalize before their format switch
// and emit a warning when the second return value is true.
package formats

import "strings"

// formatAliases maps every recognized alternate spelling (lower-case)
// to the canonical --format value. Keys must always be lower-case
// because Canonicalize lower-cases its input before lookup.
//
// Constraints enforced by tests:
//   - No alias may map to another alias (idempotence).
//   - Adding a new alias here is the entire wiring — the dispatch in
//     cmd/analyze.go runs Canonicalize before its format switch, so
//     new aliases inherit the deprecation warning automatically.
var formatAliases = map[string]string{
	"networkpolicy":         "netpol",
	"network-policy":        "netpol",
	"audit-ledger":          "audit",
	"default-deny-only":     "default-deny",
	"evidencebundle":        "evidence-bundle",
	"evidence_bundle":       "evidence-bundle",
	"cilium-network-policy": "cilium",
	"cnp":                   "cilium",
}

// Canonicalize resolves alternate spellings of --format values to
// their canonical form. The lookup is case-insensitive; the second
// return value is true only when the input matched a known alias
// (so the caller can emit a deprecation warning) and false for
// canonical names or unknown formats.
//
// Unknown formats pass through unchanged so the caller's own
// "unknown format" branch keeps producing the user-facing error —
// silent rewrites would mask typos.
func Canonicalize(format string) (string, bool) {
	canonical, ok := formatAliases[strings.ToLower(format)]
	if !ok {
		return format, false
	}
	return canonical, true
}
