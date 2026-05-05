package parser

// Per-parser version stamps. Bump whenever a parser's extraction logic
// changes (rules added/removed, evidence-line format change, confidence
// scoring change). Users pin baselines to these versions so segspec
// outputs are reproducible across upgrades.
//
// Format: MAJOR.MINOR.PATCH. Bump MINOR for new rules / behavior changes,
// PATCH for bug fixes that don't change accepted-input shape, MAJOR for
// breaking changes to evidence-line format.
const (
	VersionSpring    = "0.6.0"
	VersionCompose   = "0.6.0"
	VersionK8s       = "0.6.0"
	VersionEnvfile   = "0.6.0"
	VersionBuildfile = "0.6.0"
)

// Versions returns a map of parser format-name → version string for every
// registered parser. The returned map is a fresh copy and safe to mutate.
//
// Used by renderer.EvidenceJSON to populate the top-level `parser_versions`
// block in `--format json` output, and by the evidence-bundle export to
// stamp reproducibility metadata.
func Versions() map[string]string {
	return map[string]string{
		"spring":    VersionSpring,
		"compose":   VersionCompose,
		"k8s":       VersionK8s,
		"envfile":   VersionEnvfile,
		"buildfile": VersionBuildfile,
	}
}
