package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestParseCompose_Ports(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  web:
    image: nginx
    ports:
      - "8080:80"
      - "443"
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDepWithSource(deps, "web", "web", 80) == nil {
		t.Error("expected exposed port 80 for web service")
	}
	if findDepWithSource(deps, "web", "web", 443) == nil {
		t.Error("expected exposed port 443 for web service")
	}
}

func TestParseCompose_DependsOnList(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  app:
    image: myapp:latest
    depends_on:
      - db
      - redis
  db:
    image: postgres:15
  redis:
    image: redis:7
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dbDep := findDepWithSource(deps, "app", "db", 5432)
	if dbDep == nil {
		t.Fatal("expected app -> db:5432 dependency")
	}
	if dbDep.Description != "PostgreSQL" {
		t.Errorf("description = %q, want PostgreSQL", dbDep.Description)
	}

	redisDep := findDepWithSource(deps, "app", "redis", 6379)
	if redisDep == nil {
		t.Fatal("expected app -> redis:6379 dependency")
	}
}

func TestParseCompose_DependsOnMap(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  app:
    image: myapp:latest
    depends_on:
      db:
        condition: service_healthy
  db:
    image: mysql:8
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDepWithSource(deps, "app", "db", 3306) == nil {
		t.Error("expected app -> db:3306 dependency (MySQL inferred from image)")
	}
}

func TestParseCompose_WellKnownImages(t *testing.T) {
	tests := []struct {
		image string
		port  int
		desc  string
	}{
		{"postgres:15", 5432, "PostgreSQL"},
		{"redis:7-alpine", 6379, "Redis"},
		{"mysql:8.0", 3306, "MySQL"},
		{"mongo:6", 27017, "MongoDB"},
		{"rabbitmq:3-management", 5672, "RabbitMQ"},
		{"elasticsearch:8.10.2", 9200, "Elasticsearch"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			port, desc := inferFromImage(tt.image)
			if port != tt.port {
				t.Errorf("port = %d, want %d", port, tt.port)
			}
			if desc != tt.desc {
				t.Errorf("desc = %q, want %q", desc, tt.desc)
			}
		})
	}
}

func TestParseCompose_EnvironmentMap(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  app:
    image: myapp
    environment:
      DATABASE_URL: "jdbc:postgresql://db-server:5432/app"
      CACHE_HOST: "redis-cache:6379"
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "db-server", 5432) == nil {
		t.Error("expected dependency on db-server:5432 from environment")
	}
	if findDep(deps, "redis-cache", 6379) == nil {
		t.Error("expected dependency on redis-cache:6379 from environment")
	}
}

func TestParseCompose_EnvironmentList(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  worker:
    image: myworker
    environment:
      - "REDIS_URL=http://redis-node:6379"
      - "API_URL=http://api-service:3000/v2"
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findDep(deps, "redis-node", 6379) == nil {
		t.Error("expected dependency on redis-node:6379 from env list")
	}
	if findDep(deps, "api-service", 3000) == nil {
		t.Error("expected dependency on api-service:3000 from env list")
	}
}

func TestParseCompose_FullStack(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  frontend:
    image: node:18
    ports:
      - "3000:3000"
    depends_on:
      - backend
    environment:
      - "API_URL=http://backend:8080"
  backend:
    image: openjdk:17
    ports:
      - "8080:8080"
    depends_on:
      - postgres
      - redis
    environment:
      DATABASE_URL: "jdbc:postgresql://postgres:5432/myapp"
      REDIS_HOST: "redis:6379"
  postgres:
    image: postgres:15
    ports:
      - "5432:5432"
  redis:
    image: redis:7
    ports:
      - "6379:6379"
`
	path := filepath.Join(dir, "compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have dependencies from multiple sources
	if len(deps) == 0 {
		t.Fatal("expected multiple dependencies")
	}

	// Frontend -> backend from env
	if findDep(deps, "backend", 8080) == nil {
		t.Error("expected frontend -> backend:8080")
	}
	// Backend -> postgres from env
	if findDep(deps, "postgres", 5432) == nil {
		t.Error("expected backend -> postgres:5432")
	}
}

func TestParseCompose_ImageWithRegistry(t *testing.T) {
	port, desc := inferFromImage("docker.io/library/postgres:15")
	if port != 5432 {
		t.Errorf("port = %d, want 5432", port)
	}
	if desc != "PostgreSQL" {
		t.Errorf("desc = %q, want PostgreSQL", desc)
	}
}

func TestParseCompose_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	content := `services: {}
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps for empty services, got %d", len(deps))
	}
}

func TestParseContainerPort(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"8080", 8080},
		{"8080:80", 80},
		{"127.0.0.1:8080:80", 80},
		{"443:443/tcp", 443},
		{"invalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseContainerPort(tt.input)
			if got != tt.want {
				t.Errorf("parseContainerPort(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCompose_SourceFile(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  db:
    image: postgres:15
    ports:
      - "5432:5432"
`
	path := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(path, []byte(content), 0644)

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, d := range deps {
		if d.SourceFile != path {
			t.Errorf("SourceFile = %q, want %q", d.SourceFile, path)
		}
	}
}

// findDepWithSource is a test helper that finds a dependency by source, target, and port.
func findDepWithSource(deps []model.NetworkDependency, source, target string, port int) *model.NetworkDependency {
	for i := range deps {
		if deps[i].Source == source && deps[i].Target == target && deps[i].Port == port {
			return &deps[i]
		}
	}
	return nil
}
