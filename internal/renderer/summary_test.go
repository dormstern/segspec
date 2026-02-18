package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestSummaryBasic(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Description: "PostgreSQL database",
		Confidence: model.High, SourceFile: "application.yml",
	})
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "redis", Port: 6379,
		Protocol: "TCP", Description: "Redis cache",
		Confidence: model.Medium, SourceFile: ".env",
	})

	out := Summary(ds)

	if !strings.Contains(out, "Service: order-service") {
		t.Error("missing service name")
	}
	if !strings.Contains(out, "Dependencies: 2") {
		t.Error("missing dependency count")
	}
	if !strings.Contains(out, "postgres:5432") {
		t.Error("missing postgres dependency")
	}
	if !strings.Contains(out, "[high]") {
		t.Error("missing high confidence marker")
	}
	if !strings.Contains(out, "1 high, 1 medium, 0 low") {
		t.Error("missing confidence summary")
	}
}

func TestSummaryEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty-app")
	out := Summary(ds)
	if !strings.Contains(out, "No dependencies") {
		t.Error("expected 'No dependencies' message for empty set")
	}
}

func TestSummaryLowConfidenceWarning(t *testing.T) {
	ds := model.NewDependencySet("risky-app")
	ds.Add(model.NetworkDependency{
		Source: "risky-app", Target: "kafka", Port: 9092,
		Protocol: "TCP", Confidence: model.Low,
	})

	out := Summary(ds)
	if !strings.Contains(out, "low-confidence") {
		t.Error("missing low-confidence warning")
	}
}

func TestSummaryShowsSourceFile(t *testing.T) {
	ds := model.NewDependencySet("app")
	ds.Add(model.NetworkDependency{
		Source: "app", Target: "mongo", Port: 27017,
		Protocol: "TCP", Confidence: model.High,
		SourceFile: "docker-compose.yml",
	})

	out := Summary(ds)
	if !strings.Contains(out, "docker-compose.yml") {
		t.Error("missing source file reference")
	}
}
