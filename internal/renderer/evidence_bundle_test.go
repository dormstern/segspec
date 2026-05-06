package renderer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

// makeBundleDS returns a small DependencySet useful for several of the
// evidence-bundle tests below.
func makeBundleDS() *model.DependencySet {
	ds := model.NewDependencySet("orders")
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP",
		Confidence: model.High,
		SourceFile: "application.yml",
		EvidenceLine: "spring.datasource.url: jdbc:postgresql://postgres:5432/db",
	})
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "redis", Port: 6379, Protocol: "TCP",
		Confidence: model.Medium,
		SourceFile: "docker-compose.yml",
		EvidenceLine: "depends_on: redis",
	})
	return ds
}

// TestEvidenceBundleJSONEmpty verifies that an empty service set still emits
// a valid JSON document with all required top-level keys present and an
// empty dependencies array.
func TestEvidenceBundleJSONEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty")
	got := EvidenceBundleJSON(ds, "0.6.0-dev", nil)

	var obj map[string]any
	if err := json.Unmarshal([]byte(got), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}

	requiredKeys := []string{
		"segspec_version",
		"parser_versions",
		"input_tree_sha256",
		"generated_utc",
		"dependencies",
		"summary",
	}
	for _, k := range requiredKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("missing required top-level key %q in:\n%s", k, got)
		}
	}

	deps, ok := obj["dependencies"].([]any)
	if !ok {
		t.Fatalf("dependencies is not a JSON array: %T", obj["dependencies"])
	}
	if len(deps) != 0 {
		t.Errorf("expected empty dependencies for empty service set; got %d", len(deps))
	}

	if v, _ := obj["segspec_version"].(string); v != "0.6.0-dev" {
		t.Errorf("segspec_version = %q, want %q", v, "0.6.0-dev")
	}
}

// TestEvidenceBundleJSONDeterministic verifies that running the renderer
// twice with the same inputs produces byte-identical output. The
// generated_utc field is allowed to vary, so we strip it before comparison.
func TestEvidenceBundleJSONDeterministic(t *testing.T) {
	ds := makeBundleDS()
	files := []EvidenceBundleInputFile{
		{Path: "application.yml", Content: []byte("spring:\n  datasource:\n    url: jdbc:postgresql://postgres:5432/db\n")},
		{Path: "docker-compose.yml", Content: []byte("services:\n  orders:\n    depends_on: [redis]\n")},
	}

	a := EvidenceBundleJSON(ds, "0.6.0-dev", files)
	b := EvidenceBundleJSON(ds, "0.6.0-dev", files)

	if a == "" || b == "" {
		t.Fatalf("renderer returned empty string; expected populated JSON")
	}
	if a != b {
		t.Errorf("evidence-bundle JSON is not deterministic across two calls.\nA:\n%s\nB:\n%s", a, b)
	}
	// Sanity: must parse as JSON and contain dependencies.
	var obj map[string]any
	if err := json.Unmarshal([]byte(a), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, a)
	}
	if deps, _ := obj["dependencies"].([]any); len(deps) == 0 {
		t.Errorf("expected non-empty dependencies in determinism test; got: %v", obj["dependencies"])
	}

	// Stripping the (timestamp) generated_utc field from both should still match;
	// this confirms the body itself is byte-stable, not just by coincidence.
	stripA := stripGeneratedUTC(a)
	stripB := stripGeneratedUTC(b)
	if stripA != stripB {
		t.Errorf("evidence-bundle body (sans generated_utc) differs:\nA:\n%s\nB:\n%s", stripA, stripB)
	}
}

// TestEvidenceBundleInputTreeHashing verifies that input_tree_sha256:
//   (a) changes when any input file's CONTENT changes
//   (b) stays the same when only the input file ORDER changes
func TestEvidenceBundleInputTreeHashing(t *testing.T) {
	ds := makeBundleDS()

	filesA := []EvidenceBundleInputFile{
		{Path: "a.yml", Content: []byte("hello")},
		{Path: "b.yml", Content: []byte("world")},
	}
	// Same files, different order.
	filesAReordered := []EvidenceBundleInputFile{
		{Path: "b.yml", Content: []byte("world")},
		{Path: "a.yml", Content: []byte("hello")},
	}
	// Same paths, b's content changed.
	filesB := []EvidenceBundleInputFile{
		{Path: "a.yml", Content: []byte("hello")},
		{Path: "b.yml", Content: []byte("WORLD")},
	}

	hashA := extractInputTreeSHA(t, EvidenceBundleJSON(ds, "0.6.0-dev", filesA))
	hashAReordered := extractInputTreeSHA(t, EvidenceBundleJSON(ds, "0.6.0-dev", filesAReordered))
	hashB := extractInputTreeSHA(t, EvidenceBundleJSON(ds, "0.6.0-dev", filesB))

	if hashA != hashAReordered {
		t.Errorf("input_tree_sha256 changed when only file ORDER changed: %s vs %s", hashA, hashAReordered)
	}
	if hashA == hashB {
		t.Errorf("input_tree_sha256 did NOT change when file CONTENT changed; both = %s", hashA)
	}
	if hashA == "" {
		t.Errorf("input_tree_sha256 must be non-empty for non-empty input set")
	}
}

// TestEvidenceBundleParserVersionsPresent verifies that parser_versions
// appears in the JSON output as a map (possibly empty) — version-stamping
// is parser-side and may not be wired yet. This test guards the SHAPE of
// the field, not its contents.
func TestEvidenceBundleParserVersionsPresent(t *testing.T) {
	ds := makeBundleDS()
	out := EvidenceBundleJSON(ds, "0.6.0-dev", nil)

	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	pv, ok := obj["parser_versions"]
	if !ok {
		t.Fatalf("parser_versions key missing; got:\n%s", out)
	}
	// JSON object decodes to map[string]any. A nil map round-trips as an
	// object {} — both are acceptable. A list would be wrong.
	if _, ok := pv.(map[string]any); !ok {
		t.Errorf("parser_versions should be a JSON object/map; got %T", pv)
	}
}

// TestEvidenceBundleEvidenceBlockNonZeroLine verifies that every dependency
// in the JSON output carries an evidence block with a non-zero line, so
// downstream auditors can navigate to source from the report alone.
func TestEvidenceBundleEvidenceBlockNonZeroLine(t *testing.T) {
	ds := makeBundleDS()
	out := EvidenceBundleJSON(ds, "0.6.0-dev", nil)

	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	deps, ok := obj["dependencies"].([]any)
	if !ok || len(deps) == 0 {
		t.Fatalf("expected non-empty dependencies; got: %v", obj["dependencies"])
	}
	for i, raw := range deps {
		dep, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("dep[%d] not an object: %T", i, raw)
		}
		ev, ok := dep["evidence"].(map[string]any)
		if !ok {
			t.Errorf("dep[%d] missing evidence block: %v", i, dep)
			continue
		}
		if f, _ := ev["file"].(string); f == "" {
			t.Errorf("dep[%d].evidence.file is empty", i)
		}
		// JSON numbers decode to float64.
		ln, _ := ev["line"].(float64)
		if ln == 0 {
			t.Errorf("dep[%d].evidence.line is zero; expected non-zero", i)
		}
		if d, _ := ev["declaration"].(string); d == "" {
			t.Errorf("dep[%d].evidence.declaration is empty", i)
		}
	}
}

// TestEvidenceBundleSARIFRoot verifies that the SARIF output is valid JSON
// with the SARIF v2.1.0 root structure: $schema, version, runs.
func TestEvidenceBundleSARIFRoot(t *testing.T) {
	ds := makeBundleDS()
	out := EvidenceBundleSARIF(ds, "0.6.0-dev", nil)

	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("SARIF output is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := obj["$schema"]; !ok {
		t.Errorf("missing $schema in SARIF output:\n%s", out)
	}
	if v, _ := obj["version"].(string); v != "2.1.0" {
		t.Errorf("SARIF version = %q, want %q", v, "2.1.0")
	}
	runs, ok := obj["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("SARIF runs array missing or empty:\n%s", out)
	}
}

// TestEvidenceBundleSARIFResults verifies that each dependency becomes a
// SARIF result with level "note" and a physicalLocation.artifactLocation.
func TestEvidenceBundleSARIFResults(t *testing.T) {
	ds := makeBundleDS()
	out := EvidenceBundleSARIF(ds, "0.6.0-dev", nil)

	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	runs, _ := obj["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("no runs in SARIF output:\n%s", out)
	}
	run, _ := runs[0].(map[string]any)
	results, _ := run["results"].([]any)

	wantCount := len(ds.Dependencies())
	if len(results) != wantCount {
		t.Errorf("expected %d SARIF results, got %d:\n%s", wantCount, len(results), out)
	}

	for i, raw := range results {
		r, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("result[%d] not an object: %T", i, raw)
		}
		if lvl, _ := r["level"].(string); lvl != "note" {
			t.Errorf("result[%d] level = %q, want %q", i, lvl, "note")
		}
		locs, _ := r["locations"].([]any)
		if len(locs) == 0 {
			t.Errorf("result[%d] has no locations", i)
			continue
		}
		loc, _ := locs[0].(map[string]any)
		phys, _ := loc["physicalLocation"].(map[string]any)
		if phys == nil {
			t.Errorf("result[%d] missing physicalLocation", i)
			continue
		}
		art, _ := phys["artifactLocation"].(map[string]any)
		if art == nil {
			t.Errorf("result[%d] missing artifactLocation", i)
			continue
		}
		if uri, _ := art["uri"].(string); uri == "" {
			t.Errorf("result[%d] artifactLocation.uri is empty", i)
		}
	}

	// Verify tool name + semver.
	tool, _ := run["tool"].(map[string]any)
	driver, _ := tool["driver"].(map[string]any)
	if name, _ := driver["name"].(string); name != "segspec" {
		t.Errorf("tool.driver.name = %q, want %q", name, "segspec")
	}
	if sv, _ := driver["semanticVersion"].(string); sv != "0.6.0-dev" {
		t.Errorf("tool.driver.semanticVersion = %q, want %q", sv, "0.6.0-dev")
	}
}

// stripGeneratedUTC removes the generated_utc line from a JSON evidence
// bundle, so determinism tests can compare bodies without false negatives
// from timestamp drift.
func stripGeneratedUTC(s string) string {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, "\"generated_utc\"") {
			continue
		}
		lines = append(lines, ln)
	}
	return strings.Join(lines, "\n")
}

// extractInputTreeSHA pulls input_tree_sha256 out of a JSON evidence bundle.
func extractInputTreeSHA(t *testing.T, jsonStr string) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, jsonStr)
	}
	s, _ := obj["input_tree_sha256"].(string)
	return s
}
