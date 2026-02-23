package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestDiffRenderNoChanges(t *testing.T) {
	d := model.DependencyDiff{
		Unchanged: []model.NetworkDependency{
			{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: model.High},
		},
	}
	out := Diff(d)
	if !strings.Contains(out, "No changes detected.") {
		t.Errorf("expected 'No changes detected.', got:\n%s", out)
	}
}

func TestDiffRenderAdded(t *testing.T) {
	d := model.DependencyDiff{
		Added: []model.NetworkDependency{
			{Source: "web", Target: "redis", Port: 6379, Protocol: "TCP", Confidence: model.Medium},
		},
	}
	out := Diff(d)
	if !strings.Contains(out, "ADDED (1):") {
		t.Errorf("missing ADDED header, got:\n%s", out)
	}
	if !strings.Contains(out, "+ web -> redis:6379/TCP [medium]") {
		t.Errorf("missing added dep line, got:\n%s", out)
	}
}

func TestDiffRenderRemoved(t *testing.T) {
	d := model.DependencyDiff{
		Removed: []model.NetworkDependency{
			{Source: "web", Target: "memcached", Port: 11211, Protocol: "TCP", Confidence: model.High, SourceFile: "docker-compose.yml"},
		},
	}
	out := Diff(d)
	if !strings.Contains(out, "REMOVED (1):") {
		t.Errorf("missing REMOVED header, got:\n%s", out)
	}
	if !strings.Contains(out, "- web -> memcached:11211/TCP [high]") {
		t.Errorf("missing removed dep line, got:\n%s", out)
	}
	if !strings.Contains(out, "Was in: docker-compose.yml") {
		t.Errorf("missing 'Was in:' line, got:\n%s", out)
	}
}

func TestDiffRenderMixed(t *testing.T) {
	d := model.DependencyDiff{
		Added: []model.NetworkDependency{
			{Source: "web", Target: "elasticsearch", Port: 9200, Protocol: "TCP", Confidence: model.Medium},
		},
		Removed: []model.NetworkDependency{
			{Source: "web", Target: "memcached", Port: 11211, Protocol: "TCP", Confidence: model.High},
		},
		Unchanged: []model.NetworkDependency{
			{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: model.High},
		},
	}
	out := Diff(d)
	if !strings.Contains(out, "ADDED (1):") {
		t.Errorf("missing ADDED section, got:\n%s", out)
	}
	if !strings.Contains(out, "REMOVED (1):") {
		t.Errorf("missing REMOVED section, got:\n%s", out)
	}
	if !strings.Contains(out, "UNCHANGED: 1 dependencies") {
		t.Errorf("missing UNCHANGED count, got:\n%s", out)
	}
}

func TestDiffRenderEvidence(t *testing.T) {
	d := model.DependencyDiff{
		Added: []model.NetworkDependency{
			{
				Source:       "web",
				Target:       "elasticsearch",
				Port:         9200,
				Protocol:     "TCP",
				Confidence:   model.Medium,
				EvidenceLine: "ELASTICSEARCH_URL=http://elasticsearch:9200 (.env)",
			},
		},
	}
	out := Diff(d)
	if !strings.Contains(out, "Evidence: ELASTICSEARCH_URL=http://elasticsearch:9200 (.env)") {
		t.Errorf("missing evidence line, got:\n%s", out)
	}
}
