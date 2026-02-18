package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormorgenstern/segspec/internal/model"
)

func TestParseSpringYAML_Datasource(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  datasource:
    url: jdbc:postgresql://db-host:5432/mydb
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "db-host", 5432)
	if found == nil {
		t.Fatal("expected dependency on db-host:5432")
	}
	if found.Description != "PostgreSQL" {
		t.Errorf("description = %q, want PostgreSQL", found.Description)
	}
	if found.Confidence != model.High {
		t.Errorf("confidence = %q, want high", found.Confidence)
	}
}

func TestParseSpringYAML_Redis(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  redis:
    host: redis-cache
    port: 6379
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-cache", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-cache:6379")
	}
	if found.Description != "Redis" {
		t.Errorf("description = %q, want Redis", found.Description)
	}
}

func TestParseSpringYAML_RedisDefaultPort(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  redis:
    host: redis-server
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-server", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-server:6379 (default port)")
	}
}

func TestParseSpringYAML_Kafka(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  kafka:
    bootstrap-servers: kafka1:9092,kafka2:9093
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "kafka1", 9092) == nil {
		t.Error("expected dependency on kafka1:9092")
	}
	if findDep(deps, "kafka2", 9093) == nil {
		t.Error("expected dependency on kafka2:9093")
	}
}

func TestParseSpringYAML_RabbitMQ(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  rabbitmq:
    host: rmq-host
    port: 5672
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "rmq-host", 5672)
	if found == nil {
		t.Fatal("expected dependency on rmq-host:5672")
	}
	if found.Description != "RabbitMQ" {
		t.Errorf("description = %q, want RabbitMQ", found.Description)
	}
}

func TestParseSpringYAML_ServerPort(t *testing.T) {
	dir := t.TempDir()
	content := `server:
  port: 8080
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "self", 8080)
	if found == nil {
		t.Fatal("expected server listening port 8080")
	}
	if found.Description != "server listening port" {
		t.Errorf("description = %q, want 'server listening port'", found.Description)
	}
}

func TestParseSpringYAML_FullConfig(t *testing.T) {
	dir := t.TempDir()
	content := `server:
  port: 8443
spring:
  datasource:
    url: jdbc:mysql://mysql-primary:3306/appdb
  redis:
    host: redis-cluster
    port: 6380
  kafka:
    bootstrap-servers: broker1:9092
  rabbitmq:
    host: rabbit-server
    port: 5673
`
	path := filepath.Join(dir, "application.yaml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		target string
		port   int
	}{
		{"mysql-primary", 3306},
		{"redis-cluster", 6380},
		{"broker1", 9092},
		{"rabbit-server", 5673},
		{"self", 8443},
	}
	for _, c := range checks {
		if findDep(deps, c.target, c.port) == nil {
			t.Errorf("expected dependency on %s:%d", c.target, c.port)
		}
	}
}

func TestParseSpringProperties_Datasource(t *testing.T) {
	dir := t.TempDir()
	content := `spring.datasource.url=jdbc:postgresql://pg-host:5432/testdb
server.port=9090
`
	path := filepath.Join(dir, "application.properties")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringProperties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "pg-host", 5432) == nil {
		t.Error("expected dependency on pg-host:5432")
	}
	if findDep(deps, "self", 9090) == nil {
		t.Error("expected server port 9090")
	}
}

func TestParseSpringProperties_Redis(t *testing.T) {
	dir := t.TempDir()
	content := `spring.redis.host=redis-node
spring.redis.port=6380
`
	path := filepath.Join(dir, "application.properties")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringProperties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-node", 6380)
	if found == nil {
		t.Fatal("expected dependency on redis-node:6380")
	}
}

func TestParseSpringProperties_Kafka(t *testing.T) {
	dir := t.TempDir()
	content := `spring.kafka.bootstrap-servers=k1:9092,k2:9093
`
	path := filepath.Join(dir, "application.properties")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringProperties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "k1", 9092) == nil {
		t.Error("expected dependency on k1:9092")
	}
	if findDep(deps, "k2", 9093) == nil {
		t.Error("expected dependency on k2:9093")
	}
}

func TestParseSpringProperties_URLInValue(t *testing.T) {
	dir := t.TempDir()
	content := `custom.service.endpoint=http://api-gateway:8080/v1
`
	path := filepath.Join(dir, "application.properties")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringProperties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "api-gateway", 8080) == nil {
		t.Error("expected dependency on api-gateway:8080")
	}
}

func TestParseSpringProperties_Comments(t *testing.T) {
	dir := t.TempDir()
	content := `# This is a comment
spring.datasource.url=jdbc:postgresql://db:5432/test
! Another comment style
server.port=8080
`
	path := filepath.Join(dir, "application.properties")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringProperties(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "db", 5432) == nil {
		t.Error("expected dependency on db:5432")
	}
	if findDep(deps, "self", 8080) == nil {
		t.Error("expected server port 8080")
	}
}

func TestParseSpringYAML_SourceFile(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  datasource:
    url: jdbc:postgresql://db:5432/test
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, d := range deps {
		if d.SourceFile != path {
			t.Errorf("SourceFile = %q, want %q", d.SourceFile, path)
		}
	}
}

func TestParseSpringYAML_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	content := `# empty config
logging:
  level:
    root: INFO
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps) != 0 {
		t.Errorf("expected 0 deps for config without network settings, got %d", len(deps))
	}
}

// findDep is a test helper that finds a dependency by target and port.
func findDep(deps []model.NetworkDependency, target string, port int) *model.NetworkDependency {
	for i := range deps {
		if deps[i].Target == target && deps[i].Port == port {
			return &deps[i]
		}
	}
	return nil
}
