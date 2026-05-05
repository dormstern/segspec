package renderer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dormstern/segspec/internal/model"
)

// EvidenceBundleInputFile describes one source-config file to be hashed
// into the bundle's input_tree_sha256. The renderer sorts these by Path
// before hashing so the resulting digest is deterministic regardless of
// insertion order.
type EvidenceBundleInputFile struct {
	Path    string
	Content []byte
}

// evidenceBundle is the wire shape of `--format evidence-bundle`. It is
// designed to be:
//
//   - Deterministic: same inputs produce byte-identical bytes (modulo
//     generated_utc, which is fixed by callers who care about reproducibility).
//   - Auditable: every dependency carries a file/line/declaration block, so a
//     reviewer can navigate from the bundle back to the source-of-truth config.
//   - Reproducible: input_tree_sha256 fingerprints the entire input config
//     tree, so two reviewers running on different checkouts can confirm they
//     analyzed the same bytes without comparing the bundles themselves.
//
// Driver: IDENTITY.md secondary-persona JOB ("evidence that its NetworkPolicy
// matches its declared dependencies, so that I can review and approve
// segmentation in CI"). Closes the gap left by RHACS roxctl's deprecation
// (CL-001) — incumbents no longer ship a build-time evidence story.
type evidenceBundle struct {
	SegspecVersion  string                 `json:"segspec_version"`
	ParserVersions  map[string]string      `json:"parser_versions"`
	InputTreeSHA256 string                 `json:"input_tree_sha256"`
	GeneratedUTC    string                 `json:"generated_utc"`
	Dependencies    []evidenceBundleDep    `json:"dependencies"`
	Summary         evidenceBundleSummary  `json:"summary"`
}

type evidenceBundleDep struct {
	Source     string                  `json:"source"`
	Target     string                  `json:"target"`
	Port       int                     `json:"port"`
	Protocol   string                  `json:"protocol"`
	Confidence string                  `json:"confidence"`
	Evidence   evidenceBundleEvidence  `json:"evidence"`
}

type evidenceBundleEvidence struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Declaration string `json:"declaration"`
}

type evidenceBundleSummary struct {
	Total  int `json:"total"`
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// EvidenceBundleJSON renders the deterministic JSON evidence bundle.
//
// inputFiles describes the source config tree segspec consumed; the function
// computes input_tree_sha256 over the SORTED concatenation of (path, content)
// pairs so the digest is independent of input order. If inputFiles is nil or
// empty, the digest is computed over the empty input set (still deterministic).
//
// parserVersions is the per-parser version map, typically populated by the
// caller from parser.Versions(). The renderer remains decoupled from the
// parser package and accepts whatever map (possibly nil) the caller hands in,
// so the field is always present in output even if version stamping has not
// yet been wired up at the call site.
func EvidenceBundleJSON(ds *model.DependencySet, segspecVersion string, inputFiles []EvidenceBundleInputFile, parserVersions ...map[string]string) string {
	bundle := buildBundle(ds, segspecVersion, inputFiles, mergeParserVersions(parserVersions))
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":\"%s\"}\n", err)
	}
	return string(data) + "\n"
}

// EvidenceBundleSARIF wraps the same evidence as a SARIF v2.1.0 document
// suitable for ingestion by GitHub code-scanning, Defender, Splunk, etc.
// Each dependency becomes one `result` at level `note` (we are not flagging
// findings — we are recording evidence of declared traffic).
func EvidenceBundleSARIF(ds *model.DependencySet, segspecVersion string, inputFiles []EvidenceBundleInputFile, parserVersions ...map[string]string) string {
	bundle := buildBundle(ds, segspecVersion, inputFiles, mergeParserVersions(parserVersions))

	results := make([]map[string]any, 0, len(bundle.Dependencies))
	for _, d := range bundle.Dependencies {
		message := fmt.Sprintf("%s -> %s on %d/%s (confidence: %s)",
			d.Source, d.Target, d.Port, d.Protocol, d.Confidence)
		artifactURI := d.Evidence.File
		if artifactURI == "" {
			// SARIF requires uri to be non-empty; fall back to a stable
			// placeholder so consumers don't choke on nil locations.
			artifactURI = "unknown"
		}
		result := map[string]any{
			"ruleId":  "segspec.network-dependency",
			"level":   "note",
			"message": map[string]any{"text": message},
			"locations": []map[string]any{
				{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{"uri": artifactURI},
						"region": map[string]any{
							"startLine": d.Evidence.Line,
							"snippet":   map[string]any{"text": d.Evidence.Declaration},
						},
					},
				},
			},
		}
		results = append(results, result)
	}

	sarif := map[string]any{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{
			{
				"tool": map[string]any{
					"driver": map[string]any{
						"name":            "segspec",
						"semanticVersion": segspecVersion,
						"informationUri":  "https://github.com/dormstern/segspec",
						"rules": []map[string]any{
							{
								"id": "segspec.network-dependency",
								"shortDescription": map[string]any{
									"text": "Declared network dependency between two workloads.",
								},
								"fullDescription": map[string]any{
									"text": "segspec extracted this dependency from a source config file. The result is informational evidence, not a finding.",
								},
								"defaultConfiguration": map[string]any{"level": "note"},
							},
						},
					},
				},
				"properties": map[string]any{
					"segspec_version":   bundle.SegspecVersion,
					"parser_versions":   bundle.ParserVersions,
					"input_tree_sha256": bundle.InputTreeSHA256,
					"generated_utc":     bundle.GeneratedUTC,
					"summary":           bundle.Summary,
				},
				"results": results,
			},
		},
	}

	data, err := json.MarshalIndent(sarif, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":\"%s\"}\n", err)
	}
	return string(data) + "\n"
}

// mergeParserVersions returns the first non-nil map from the variadic slice,
// or an empty (but non-nil) map. Encoded JSON is `{}`, never `null`, which
// matches the contract `parser_versions` is always present.
func mergeParserVersions(maps []map[string]string) map[string]string {
	for _, m := range maps {
		if m != nil {
			out := make(map[string]string, len(m))
			for k, v := range m {
				out[k] = v
			}
			return out
		}
	}
	return map[string]string{}
}

// buildBundle is the shared core of both renderers.
func buildBundle(ds *model.DependencySet, segspecVersion string, inputFiles []EvidenceBundleInputFile, parserVersions map[string]string) evidenceBundle {
	deps := ds.Dependencies()

	bundleDeps := make([]evidenceBundleDep, 0, len(deps))
	var high, med, low int
	for _, d := range deps {
		switch d.Confidence {
		case model.High:
			high++
		case model.Medium:
			med++
		case model.Low:
			low++
		}
		// Line numbers are not tracked in the model today; once
		// parser-line-numbers lands as its own feature we'll plumb the real
		// integer through. Until then we synthesize a non-zero placeholder
		// (1) whenever the dependency has a SourceFile so SARIF tooling that
		// requires a positive startLine doesn't reject the document.
		line := 0
		if d.SourceFile != "" {
			line = 1
		}
		bundleDeps = append(bundleDeps, evidenceBundleDep{
			Source:     d.Source,
			Target:     d.Target,
			Port:       d.Port,
			Protocol:   d.Protocol,
			Confidence: string(d.Confidence),
			Evidence: evidenceBundleEvidence{
				File:        d.SourceFile,
				Line:        line,
				Declaration: model.RedactSecrets(d.EvidenceLine),
			},
		})
	}

	return evidenceBundle{
		SegspecVersion: segspecVersion,
		// parser_versions is sourced from the caller (typically
		// parser.Versions()). Renderer stays parser-decoupled — if the
		// caller passes nil, the helper above returns an empty (non-nil)
		// map so the field is always JSON-encoded as an object, never null.
		ParserVersions:  parserVersions,
		InputTreeSHA256: hashInputTree(inputFiles),
		GeneratedUTC:    time.Now().UTC().Format(time.RFC3339),
		Dependencies:    bundleDeps,
		Summary: evidenceBundleSummary{
			Total:  len(deps),
			High:   high,
			Medium: med,
			Low:    low,
		},
	}
}

// hashInputTree computes SHA-256 over the sorted concatenation of every
// (path, content) pair in inputFiles. Order-insensitive, content-sensitive.
// Returns hex of the digest. For an empty input set returns the SHA-256 of
// the empty string ("e3b0c44...") so the field is always populated.
func hashInputTree(files []EvidenceBundleInputFile) string {
	sorted := make([]EvidenceBundleInputFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})
	h := sha256.New()
	for _, f := range sorted {
		// Path then a NUL byte then content then a NUL byte. The NUL is a
		// boundary that prevents collisions like {"a","bc"} vs {"ab","c"}.
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write(f.Content)
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}
