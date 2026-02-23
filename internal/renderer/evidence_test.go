package renderer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestEvidenceBasic(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Description: "PostgreSQL database",
		Confidence: model.High, SourceFile: "application.yml",
		EvidenceLine: "spring.datasource.url: jdbc:postgresql://postgres:5432/app",
	})
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "redis", Port: 6379,
		Protocol: "TCP", Description: "Redis cache",
		Confidence: model.Medium, SourceFile: ".env",
		EvidenceLine: "REDIS_URL=redis://redis:6379",
	})

	out := Evidence(ds)

	if !strings.Contains(out, "# Network Dependency Evidence Report") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "Total: 2 | High: 1 | Medium: 1 | Low: 0") {
		t.Error("missing or incorrect summary counts")
	}
	if !strings.Contains(out, "postgres:5432/TCP [HIGH]") {
		t.Error("missing postgres dependency")
	}
	if !strings.Contains(out, "redis:6379/TCP [MEDIUM]") {
		t.Error("missing redis dependency")
	}
	if !strings.Contains(out, "Evidence: `spring.datasource.url") {
		t.Error("missing postgres evidence line")
	}
	if !strings.Contains(out, "Evidence: `REDIS_URL=redis://redis:6379`") {
		t.Error("missing redis evidence line")
	}
}

func TestEvidenceEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty-app")
	out := Evidence(ds)
	if out != "No dependencies found.\n" {
		t.Errorf("expected 'No dependencies found.\\n', got %q", out)
	}
}

func TestEvidenceLowConfWarning(t *testing.T) {
	ds := model.NewDependencySet("risky-app")
	ds.Add(model.NetworkDependency{
		Source: "risky-app", Target: "kafka", Port: 9092,
		Protocol: "TCP", Description: "Kafka broker",
		Confidence: model.Low, SourceFile: "build.gradle",
		EvidenceLine: "implementation 'org.apache.kafka:kafka-clients'",
	})

	out := Evidence(ds)
	if !strings.Contains(out, "[LOW] \u26a0") {
		t.Error("missing low confidence warning marker")
	}
}

func TestEvidenceNoEvidenceLine(t *testing.T) {
	ds := model.NewDependencySet("app")
	ds.Add(model.NetworkDependency{
		Source: "app", Target: "mongo", Port: 27017,
		Protocol: "TCP", Description: "MongoDB",
		Confidence: model.High, SourceFile: "docker-compose.yml",
	})

	out := Evidence(ds)
	if !strings.Contains(out, "Evidence: (no direct config line)") {
		t.Error("missing '(no direct config line)' for empty evidence")
	}
}

func TestEvidenceJSONBasic(t *testing.T) {
	ds := model.NewDependencySet("order-service")
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "postgres", Port: 5432,
		Protocol: "TCP", Description: "PostgreSQL database",
		Confidence: model.High, SourceFile: "application.yml",
		EvidenceLine: "spring.datasource.url: jdbc:postgresql://postgres:5432/app",
		ServiceType: "database",
	})
	ds.Add(model.NetworkDependency{
		Source: "order-service", Target: "redis", Port: 6379,
		Protocol: "TCP", Description: "Redis cache",
		Confidence: model.Medium, SourceFile: ".env",
		EvidenceLine: "REDIS_URL=redis://redis:6379",
		ServiceType: "cache",
	})

	out := EvidenceJSON(ds)

	var report evidenceReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report.Service != "order-service" {
		t.Errorf("service = %q, want order-service", report.Service)
	}
	if report.Summary.Total != 2 {
		t.Errorf("summary.total = %d, want 2", report.Summary.Total)
	}
	if report.Summary.High != 1 {
		t.Errorf("summary.high = %d, want 1", report.Summary.High)
	}
	if report.Summary.Medium != 1 {
		t.Errorf("summary.medium = %d, want 1", report.Summary.Medium)
	}
	if len(report.Dependencies) != 2 {
		t.Errorf("dependencies count = %d, want 2", len(report.Dependencies))
	}
	if report.Version != "0.5.0" {
		t.Errorf("version = %q, want 0.5.0", report.Version)
	}
}

func TestEvidenceJSONEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty-app")
	out := EvidenceJSON(ds)
	if out != "{}\n" {
		t.Errorf("expected '{}\\n', got %q", out)
	}
}

func TestEvidenceJSONParses(t *testing.T) {
	ds := model.NewDependencySet("web")
	ds.Add(model.NetworkDependency{
		Source: "web", Target: "api", Port: 8080,
		Protocol: "TCP", Description: "API gateway",
		Confidence: model.High, SourceFile: "nginx.conf",
		EvidenceLine: "proxy_pass http://api:8080;",
	})

	out := EvidenceJSON(ds)

	var report evidenceReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(report.Dependencies) != 1 {
		t.Fatalf("dependencies count = %d, want 1", len(report.Dependencies))
	}
	dep := report.Dependencies[0]
	if dep.Target != "api" {
		t.Errorf("target = %q, want api", dep.Target)
	}
	if dep.Port != 8080 {
		t.Errorf("port = %d, want 8080", dep.Port)
	}
	if dep.EvidenceLine != "proxy_pass http://api:8080;" {
		t.Errorf("evidence_line = %q, want proxy_pass line", dep.EvidenceLine)
	}
}
