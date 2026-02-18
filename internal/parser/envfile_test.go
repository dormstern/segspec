package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestParseEnvFile_DatabaseURL(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=jdbc:postgresql://db-host:5432/myapp
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "db-host", 5432)
	if found == nil {
		t.Fatal("expected dependency on db-host:5432")
	}
	if found.Description != "database" {
		t.Errorf("description = %q, want database", found.Description)
	}
	if found.Confidence != model.Medium {
		t.Errorf("confidence = %q, want medium", found.Confidence)
	}
}

func TestParseEnvFile_RedisURL(t *testing.T) {
	dir := t.TempDir()
	content := `REDIS_URL=http://redis-host:6379
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-host", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-host:6379")
	}
	if found.Description != "Redis" {
		t.Errorf("description = %q, want Redis", found.Description)
	}
}

func TestParseEnvFile_MultipleVars(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=jdbc:postgresql://pg:5432/db
REDIS_HOST=redis-server:6379
KAFKA_BROKERS=kafka1:9092
ELASTICSEARCH_URL=http://es-node:9200
API_URL=http://api-gateway:8080/v1
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		target string
		port   int
	}{
		{"pg", 5432},
		{"redis-server", 6379},
		{"kafka1", 9092},
		{"es-node", 9200},
		{"api-gateway", 8080},
	}
	for _, c := range checks {
		if findDep(deps, c.target, c.port) == nil {
			t.Errorf("expected dependency on %s:%d", c.target, c.port)
		}
	}
}

func TestParseEnvFile_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL="jdbc:postgresql://quoted-host:5432/db"
REDIS_URL='http://single-quoted:6379'
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "quoted-host", 5432) == nil {
		t.Error("expected dependency from double-quoted value")
	}
	if findDep(deps, "single-quoted", 6379) == nil {
		t.Error("expected dependency from single-quoted value")
	}
}

func TestParseEnvFile_Comments(t *testing.T) {
	dir := t.TempDir()
	content := `# This is a comment
DATABASE_URL=jdbc:postgresql://db:5432/test

# Another comment
REDIS_URL=http://redis:6379
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "db", 5432) == nil {
		t.Error("expected dependency on db:5432")
	}
	if findDep(deps, "redis", 6379) == nil {
		t.Error("expected dependency on redis:6379")
	}
}

func TestParseEnvFile_EmptyValues(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=
REDIS_URL=
SOME_FLAG=true
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deps) != 0 {
		t.Errorf("expected 0 deps for empty/non-URL values, got %d", len(deps))
	}
}

func TestParseEnvFile_UnknownVarWithURL(t *testing.T) {
	dir := t.TempDir()
	content := `MY_CUSTOM_SERVICE_URL=http://custom-service:9090/api
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "custom-service", 9090)
	if found == nil {
		t.Fatal("expected dependency on custom-service:9090 from unknown var with URL value")
	}
	// Should use suffix-based heuristic for _URL
	if found.Description != "service" {
		t.Errorf("description = %q, want service", found.Description)
	}
}

func TestParseEnvFile_JDBCURL(t *testing.T) {
	dir := t.TempDir()
	content := `DB_URL=jdbc:mysql://mysql-host:3306/production
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "mysql-host", 3306)
	if found == nil {
		t.Fatal("expected dependency on mysql-host:3306")
	}
	if found.Description != "database" {
		t.Errorf("description = %q, want database", found.Description)
	}
}

func TestParseEnvFile_MongoDBURI(t *testing.T) {
	dir := t.TempDir()
	content := `MONGODB_URI=http://mongo-primary:27017/appdb
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "mongo-primary", 27017)
	if found == nil {
		t.Fatal("expected dependency on mongo-primary:27017")
	}
	if found.Description != "MongoDB" {
		t.Errorf("description = %q, want MongoDB", found.Description)
	}
}

func TestParseEnvFile_Deduplication(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=jdbc:postgresql://db:5432/app1
DB_URL=jdbc:postgresql://db:5432/app2
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, d := range deps {
		if d.Target == "db" && d.Port == 5432 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduplicated dep for db:5432, got %d", count)
	}
}

func TestParseEnvFile_SourceFile(t *testing.T) {
	dir := t.TempDir()
	content := `DATABASE_URL=jdbc:postgresql://db:5432/test
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, d := range deps {
		if d.SourceFile != path {
			t.Errorf("SourceFile = %q, want %q", d.SourceFile, path)
		}
	}
}

func TestParseEnvFile_BareHostPort(t *testing.T) {
	dir := t.TempDir()
	content := `REDIS_HOST=redis-bare:6379
`
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := findDep(deps, "redis-bare", 6379)
	if found == nil {
		t.Fatal("expected dependency on redis-bare:6379 from bare host:port")
	}
}
