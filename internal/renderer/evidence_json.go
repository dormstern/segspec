package renderer

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
)

type evidenceReport struct {
	Service        string                    `json:"service"`
	Generated      string                    `json:"generated"`
	Version        string                    `json:"version"`
	ParserVersions map[string]string         `json:"parser_versions"`
	Summary        evidenceSummary           `json:"summary"`
	Dependencies   []model.NetworkDependency `json:"dependencies"`
}

type evidenceSummary struct {
	Total  int `json:"total"`
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// EvidenceJSON renders a JSON evidence report.
func EvidenceJSON(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		// Even with zero deps we stamp parser_versions so downstream
		// tooling (baselines, evidence bundles) can verify which parser
		// versions ran and confirm the empty result is reproducible.
		empty := evidenceReport{
			ParserVersions: parser.Versions(),
			Dependencies:   []model.NetworkDependency{},
		}
		data, err := json.MarshalIndent(empty, "", "  ")
		if err != nil {
			return fmt.Sprintf("{\"error\": \"%s\"}\n", err)
		}
		return string(data) + "\n"
	}

	// Redact secrets from evidence lines before serializing.
	redacted := make([]model.NetworkDependency, len(deps))
	copy(redacted, deps)
	for i := range redacted {
		redacted[i].EvidenceLine = model.RedactSecrets(redacted[i].EvidenceLine)
	}

	var highCount, medCount, lowCount int
	for _, dep := range deps {
		switch dep.Confidence {
		case model.High:
			highCount++
		case model.Medium:
			medCount++
		case model.Low:
			lowCount++
		}
	}

	report := evidenceReport{
		Service:        ds.ServiceName,
		Generated:      time.Now().Format("2006-01-02"),
		Version:        "0.6.0",
		ParserVersions: parser.Versions(),
		Summary: evidenceSummary{
			Total:  len(deps),
			High:   highCount,
			Medium: medCount,
			Low:    lowCount,
		},
		Dependencies: redacted,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\": \"%s\"}\n", err)
	}
	return string(data) + "\n"
}
