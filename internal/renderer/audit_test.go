package renderer

import (
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestAuditEmpty(t *testing.T) {
	ds := model.NewDependencySet("empty")
	got := Audit(ds)
	if got != "No dependencies found.\n" {
		t.Errorf("expected empty marker, got %q", got)
	}
}

func TestAuditHappyPath(t *testing.T) {
	ds := model.NewDependencySet("orders")
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "postgres", Port: 5432, Protocol: "TCP",
		Description: "PostgreSQL database",
		Confidence:  model.High,
		SourceFile:  "application.yml",
		EvidenceLine: "spring.datasource.url: jdbc:postgresql://postgres:5432/app",
	})
	ds.Add(model.NetworkDependency{
		Source: "orders", Target: "redis", Port: 6379, Protocol: "TCP",
		Description: "Redis cache",
		Confidence:  model.Medium,
		SourceFile:  ".env",
		EvidenceLine: "REDIS_URL=redis://redis:6379",
	})
	ds.Add(model.NetworkDependency{
		Source: "checkout", Target: "orders", Port: 8080, Protocol: "TCP",
		Description: "checkout calls orders API",
		Confidence:  model.High,
		SourceFile:  "checkout/application.yml",
		EvidenceLine: "orders.api.url: http://orders:8080",
	})

	got := Audit(ds)

	want := []string{
		"# Network Dependency Audit Ledger",
		"Service: `orders`",
		"## Summary",
		"| Workloads with declared traffic | 4 |",
		"| Total dependencies | 3 |",
		"| High confidence (auto-approve candidates) | 2 |",
		"| Medium confidence (review) | 1 |",
		"## Workload sign-off",
		"### `orders`",
		"**Egress**",
		"| `postgres` | `5432/TCP` | HIGH (approve)",
		"| `redis` | `6379/TCP` | MEDIUM (review)",
		"**Ingress**",
		"| `checkout` | `8080/TCP` | HIGH (approve)",
		"### `checkout`",
		"### `postgres`",
		"### `redis`",
		"## Auditor checklist",
		"- [ ] Every workload above has a corresponding `NetworkPolicy`",
		"Run fingerprint:",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("audit output missing %q", w)
		}
	}

	// Status badges
	if !strings.Contains(got, "review recommended") {
		t.Errorf("expected medium-confidence workload to surface a 'review recommended' status badge")
	}
}

func TestAuditLowConfidenceTriggersReviewRequired(t *testing.T) {
	ds := model.NewDependencySet("risky")
	ds.Add(model.NetworkDependency{
		Source: "risky", Target: "kafka", Port: 9092, Protocol: "TCP",
		Description: "Kafka broker (inferred)",
		Confidence:  model.Low,
		SourceFile:  "build.gradle",
		EvidenceLine: "implementation 'org.apache.kafka:kafka-clients'",
	})
	out := Audit(ds)
	if !strings.Contains(out, "REVIEW REQUIRED") {
		t.Errorf("expected REVIEW REQUIRED status for low-confidence workload, got:\n%s", out)
	}
	if !strings.Contains(out, "LOW (investigate)") {
		t.Errorf("expected LOW (investigate) row label")
	}
	if !strings.Contains(out, "Each of the 1 LOW-confidence row(s) has been confirmed") {
		t.Errorf("expected per-low-count checklist line")
	}
}

func TestAuditRedactsSecrets(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{
		Source: "svc", Target: "db", Port: 5432, Protocol: "TCP",
		Description: "DB",
		Confidence:  model.High,
		SourceFile:  ".env",
		EvidenceLine: "DATABASE_PASSWORD=hunter2supersecret",
	})
	out := Audit(ds)
	if strings.Contains(out, "hunter2supersecret") {
		t.Errorf("audit output leaked secret value:\n%s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in audit output:\n%s", out)
	}
}

func TestAuditFingerprintIsStable(t *testing.T) {
	build := func() *model.DependencySet {
		ds := model.NewDependencySet("svc")
		ds.Add(model.NetworkDependency{
			Source: "svc", Target: "db", Port: 5432, Protocol: "TCP",
			Confidence: model.High, SourceFile: "a.yml",
			EvidenceLine: "url: db:5432",
		})
		ds.Add(model.NetworkDependency{
			Source: "svc", Target: "cache", Port: 6379, Protocol: "TCP",
			Confidence: model.Medium, SourceFile: "b.yml",
			EvidenceLine: "url: cache:6379",
		})
		return ds
	}
	a := auditFingerprint(build())
	b := auditFingerprint(build())
	if a != b {
		t.Errorf("fingerprint not stable: %q vs %q", a, b)
	}
	// Order-independent: adding in reverse should yield same fingerprint
	// because Dependencies() sorts.
	ds2 := model.NewDependencySet("svc")
	ds2.Add(model.NetworkDependency{
		Source: "svc", Target: "cache", Port: 6379, Protocol: "TCP",
		Confidence: model.Medium, SourceFile: "b.yml",
		EvidenceLine: "url: cache:6379",
	})
	ds2.Add(model.NetworkDependency{
		Source: "svc", Target: "db", Port: 5432, Protocol: "TCP",
		Confidence: model.High, SourceFile: "a.yml",
		EvidenceLine: "url: db:5432",
	})
	if got := auditFingerprint(ds2); got != a {
		t.Errorf("fingerprint depends on insertion order: %q vs %q", got, a)
	}
}

func TestAuditEvidenceWithPipeIsEscaped(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{
		Source: "svc", Target: "host", Port: 80, Protocol: "TCP",
		Confidence: model.High, SourceFile: "config",
		EvidenceLine: "value: a | b",
	})
	out := Audit(ds)
	// Make sure the literal pipe inside evidence is escaped so the table
	// renders correctly.
	if strings.Contains(out, "a | b`") {
		t.Errorf("unescaped pipe found in audit table evidence:\n%s", out)
	}
	if !strings.Contains(out, `a \| b`) {
		t.Errorf("expected escaped pipe in audit table evidence:\n%s", out)
	}
}

func TestAuditNoEvidenceCellHandled(t *testing.T) {
	ds := model.NewDependencySet("svc")
	ds.Add(model.NetworkDependency{
		Source: "svc", Target: "ext", Port: 443, Protocol: "TCP",
		Confidence: model.High,
		// no SourceFile, no EvidenceLine
	})
	out := Audit(ds)
	if !strings.Contains(out, "_no direct config line_") {
		t.Errorf("expected fallback cell for missing evidence:\n%s", out)
	}
	if !strings.Contains(out, "Rows without direct evidence line | 1") {
		t.Errorf("expected summary count for missing evidence:\n%s", out)
	}
	if !strings.Contains(out, "1 row(s) without a direct config line have been traced") {
		t.Errorf("expected checklist line for missing-evidence count:\n%s", out)
	}
}
