package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormstern/segspec/internal/model"
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

// --- Fix 1: JDBC URLs without port ---

func TestParseJDBC_PostgresNoPort(t *testing.T) {
	dep, ok := parseJDBC("jdbc:postgresql://db-host/mydb", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match JDBC URL without port")
	}
	if dep.Target != "db-host" {
		t.Errorf("target = %q, want db-host", dep.Target)
	}
	if dep.Port != 5432 {
		t.Errorf("port = %d, want 5432 (default for postgresql)", dep.Port)
	}
	if dep.Confidence != model.Medium {
		t.Errorf("confidence = %q, want medium (port was inferred)", dep.Confidence)
	}
	if dep.Description != "PostgreSQL" {
		t.Errorf("description = %q, want PostgreSQL", dep.Description)
	}
}

func TestParseJDBC_MysqlNoPort(t *testing.T) {
	dep, ok := parseJDBC("jdbc:mysql://mysql-host/app", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match JDBC URL without port")
	}
	if dep.Target != "mysql-host" {
		t.Errorf("target = %q, want mysql-host", dep.Target)
	}
	if dep.Port != 3306 {
		t.Errorf("port = %d, want 3306 (default for mysql)", dep.Port)
	}
	if dep.Confidence != model.Medium {
		t.Errorf("confidence = %q, want medium (port was inferred)", dep.Confidence)
	}
}

func TestParseJDBC_H2MemSkipped(t *testing.T) {
	_, ok := parseJDBC("jdbc:h2:mem:testdb", "test.yml")
	if ok {
		t.Fatal("expected h2:mem to be skipped (in-memory DB, not a network dep)")
	}
}

func TestParseJDBC_ExplicitPortStillHigh(t *testing.T) {
	dep, ok := parseJDBC("jdbc:postgresql://db-host:5432/mydb", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match")
	}
	if dep.Confidence != model.High {
		t.Errorf("confidence = %q, want high (port was explicit)", dep.Confidence)
	}
}

func TestParseJDBC_OracleNoPort(t *testing.T) {
	dep, ok := parseJDBC("jdbc:oracle://oracle-host/orcl", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match JDBC URL without port")
	}
	if dep.Port != 1521 {
		t.Errorf("port = %d, want 1521 (default for oracle)", dep.Port)
	}
}

func TestParseJDBC_SqlServerNoPort(t *testing.T) {
	dep, ok := parseJDBC("jdbc:sqlserver://sql-host/mydb", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match JDBC URL without port")
	}
	if dep.Port != 1433 {
		t.Errorf("port = %d, want 1433 (default for sqlserver)", dep.Port)
	}
}

func TestParseJDBC_MariadbNoPort(t *testing.T) {
	dep, ok := parseJDBC("jdbc:mariadb://maria-host/appdb", "test.yml")
	if !ok {
		t.Fatal("expected parseJDBC to match JDBC URL without port")
	}
	if dep.Port != 3306 {
		t.Errorf("port = %d, want 3306 (default for mariadb)", dep.Port)
	}
}

func TestParseJDBC_DerbySkipped(t *testing.T) {
	_, ok := parseJDBC("jdbc:derby:memory:testdb;create=true", "test.yml")
	if ok {
		t.Fatal("expected derby to be skipped (embedded DB, not a network dep)")
	}
}

func TestParseSpringYAML_JDBCNoPort(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  datasource:
    url: jdbc:postgresql://db-host/mydb
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "db-host", 5432)
	if found == nil {
		t.Fatal("expected dependency on db-host:5432 (default port for postgresql)")
	}
	if found.Confidence != model.Medium {
		t.Errorf("confidence = %q, want medium (port was inferred)", found.Confidence)
	}
}

// --- Fix 2: Multi-document YAML (profiles) ---

func TestParseSpringYAML_MultiDocument(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  datasource:
    url: jdbc:postgresql://db-default:5432/appdb
server:
  port: 8080
---
spring:
  profiles: dev
  datasource:
    url: jdbc:postgresql://db-dev:5432/appdb_dev
  redis:
    host: redis-dev
    port: 6379
---
spring:
  profiles: prod
  datasource:
    url: jdbc:mysql://db-prod:3306/appdb_prod
  redis:
    host: redis-prod
    port: 6380
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find deps from ALL documents
	if findDep(deps, "db-default", 5432) == nil {
		t.Error("expected dependency on db-default:5432 from first document")
	}
	if findDep(deps, "db-dev", 5432) == nil {
		t.Error("expected dependency on db-dev:5432 from dev profile")
	}
	if findDep(deps, "redis-dev", 6379) == nil {
		t.Error("expected dependency on redis-dev:6379 from dev profile")
	}
	if findDep(deps, "db-prod", 3306) == nil {
		t.Error("expected dependency on db-prod:3306 from prod profile")
	}
	if findDep(deps, "redis-prod", 6380) == nil {
		t.Error("expected dependency on redis-prod:6380 from prod profile")
	}
	if findDep(deps, "self", 8080) == nil {
		t.Error("expected server port 8080 from first document")
	}
}

func TestParseSpringYAML_SpringBoot3RedisKey(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  data:
    redis:
      host: redis-boot3
      port: 6379
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-boot3", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-boot3:6379 (Spring Boot 3.x spring.data.redis.host)")
	}
	if found.Description != "Redis" {
		t.Errorf("description = %q, want Redis", found.Description)
	}
}

func TestParseSpringYAML_SpringBoot3RedisDefaultPort(t *testing.T) {
	dir := t.TempDir()
	content := `spring:
  data:
    redis:
      host: redis-boot3-noport
`
	path := filepath.Join(dir, "application.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-boot3-noport", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-boot3-noport:6379 (default port)")
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
