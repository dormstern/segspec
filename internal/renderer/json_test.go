package renderer

import (
	"encoding/json"
	"testing"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
)

// TestEvidenceJSONIncludesParserVersions verifies that the `--format json`
// output (rendered via EvidenceJSON) includes a top-level "parser_versions"
// block whose contents match parser.Versions().
func TestEvidenceJSONIncludesParserVersions(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{
		Source: "svc", Target: "postgres", Port: 5432,
		Protocol: "TCP", Description: "PostgreSQL",
		Confidence: model.High, SourceFile: "application.yml",
		EvidenceLine: "spring.datasource.url: jdbc:postgresql://postgres:5432/app",
	})

	out := EvidenceJSON(ds)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	pvRaw, ok := raw["parser_versions"]
	if !ok {
		t.Fatal("expected top-level \"parser_versions\" field in JSON output")
	}

	var pv map[string]string
	if err := json.Unmarshal(pvRaw, &pv); err != nil {
		t.Fatalf("parser_versions is not map[string]string: %v", err)
	}

	want := parser.Versions()
	if len(pv) != len(want) {
		t.Errorf("parser_versions has %d entries, want %d", len(pv), len(want))
	}
	for name, ver := range want {
		if got := pv[name]; got != ver {
			t.Errorf("parser_versions[%q] = %q, want %q", name, got, ver)
		}
	}
}
