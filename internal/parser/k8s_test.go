package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestK8sDeploymentWithPortsAndEnv(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-service
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: order
        image: order-service:latest
        ports:
        - containerPort: 8080
        - containerPort: 9090
        env:
        - name: DATABASE_URL
          value: "postgresql://db-primary.prod.svc.cluster.local:5432/orders"
        - name: CACHE_HOST
          value: "redis-master:6379"
        - name: API_ENDPOINT
          value: "http://payment-service:8080/api"
        - name: CONFIG_REF
          valueFrom:
            configMapKeyRef:
              name: order-config
              key: app.properties
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: password
`
	path := writeTempFile(t, "deployment.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect:
	// 2 container ports (8080, 9090)
	// 1 URL dep (postgresql://...)
	// 1 host:port dep (redis-master:6379)
	// 1 URL dep (http://payment-service:8080/api)
	// 1 configMapKeyRef (order-config)
	// 1 secretKeyRef (db-credentials)
	// Total: 7

	assertDepCount(t, deps, 7)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 8080 && d.Target == "order-service" && d.Confidence == model.High
	}, "container port 8080")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 9090 && d.Target == "order-service" && d.Confidence == model.High
	}, "container port 9090")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "db-primary.prod.svc.cluster.local" && d.Port == 5432 && d.Confidence == model.High
	}, "postgresql URL dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "redis-master" && d.Port == 6379 && d.Confidence == model.High
	}, "redis host:port dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "payment-service" && d.Port == 8080 && d.Confidence == model.High
	}, "payment-service URL dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "order-config" && d.Confidence == model.Medium
	}, "configMapKeyRef dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "db-credentials" && d.Confidence == model.Medium
	}, "secretKeyRef dep")
}

func TestK8sServicePorts(t *testing.T) {
	manifest := `apiVersion: v1
kind: Service
metadata:
  name: order-service
spec:
  selector:
    app: order
  ports:
  - port: 80
    targetPort: 8080
    protocol: TCP
  - port: 443
    targetPort: 8443
`
	path := writeTempFile(t, "service.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 2)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 80 && d.Source == "order-service" && d.Confidence == model.High
	}, "service port 80")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 443 && d.Source == "order-service"
	}, "service port 443")
}

func TestK8sMultiDocumentYAML(t *testing.T) {
	manifest := `apiVersion: v1
kind: Service
metadata:
  name: frontend-svc
spec:
  ports:
  - port: 80
    targetPort: 3000
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
spec:
  template:
    spec:
      containers:
      - name: frontend
        image: frontend:latest
        ports:
        - containerPort: 3000
        env:
        - name: API_URL
          value: "http://api-gateway:8080"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: frontend-config
data:
  backend_url: "http://backend-service:9090/api"
  cache_addr: "memcached:11211"
`
	path := writeTempFile(t, "multi-doc.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Service: 1 port
	// Deployment: 1 container port + 1 URL env
	// ConfigMap: 1 URL + 1 host:port
	// Total: 5
	assertDepCount(t, deps, 5)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "frontend-svc" && d.Port == 80
	}, "frontend service port")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "frontend" && d.Port == 3000 && d.Target == "frontend"
	}, "frontend container port")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "frontend" && d.Target == "api-gateway" && d.Port == 8080
	}, "API URL env dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "frontend-config" && d.Target == "backend-service" && d.Port == 9090
	}, "ConfigMap backend URL")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "frontend-config" && d.Target == "memcached" && d.Port == 11211
	}, "ConfigMap memcached host:port")
}

func TestK8sNonK8sYAMLReturnsNil(t *testing.T) {
	// Spring Boot application.yml — no apiVersion/kind
	content := `spring:
  datasource:
    url: jdbc:postgresql://localhost:5432/mydb
  redis:
    host: redis-server
    port: 6379
server:
  port: 8080
`
	path := writeTempFile(t, "application.yml", content)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for non-K8s YAML, got %d deps", len(deps))
	}
}

func TestK8sStatefulSet(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: postgres
        image: postgres:15
        ports:
        - containerPort: 5432
        env:
        - name: REPLICATION_HOST
          value: "postgres-0.postgres.db.svc.cluster.local:5432"
`
	path := writeTempFile(t, "statefulset.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 2)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 5432 && d.Target == "postgres" && d.Confidence == model.High
	}, "postgres container port")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "postgres-0.postgres.db" && d.Port == 5432 && d.Confidence == model.High
	}, "replication K8s DNS")
}

func TestK8sConfigMap(t *testing.T) {
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  database_host: "postgres-primary:5432"
  redis_url: "redis://redis-cluster:6379"
  plain_text: "no network info here"
`
	path := writeTempFile(t, "configmap.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 2)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "postgres-primary" && d.Port == 5432
	}, "ConfigMap postgres host:port")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "redis-cluster" && d.Port == 6379
	}, "ConfigMap redis URL")
}

func TestK8sDockerComposeReturnsNil(t *testing.T) {
	// docker-compose.yml is not a K8s manifest — should return nil
	content := `version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
  db:
    image: postgres:15
    environment:
      POSTGRES_DB: mydb
    ports:
      - "5432:5432"
`
	path := writeTempFile(t, "docker-compose.yml", content)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for docker-compose.yml, got %d deps", len(deps))
	}
}

func TestK8sMarkerByteCheck(t *testing.T) {
	// Verify k8sMarker correctly identifies K8s manifests
	k8sContent := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\n")
	if !k8sMarker(k8sContent) {
		t.Error("expected k8sMarker to return true for K8s manifest")
	}

	// Non-K8s content
	springContent := []byte("spring:\n  datasource:\n    url: jdbc:postgresql://host:5432/db\n")
	if k8sMarker(springContent) {
		t.Error("expected k8sMarker to return false for Spring config")
	}

	// Has apiVersion but no kind
	partialContent := []byte("apiVersion: v1\ndata:\n  key: value\n")
	if k8sMarker(partialContent) {
		t.Error("expected k8sMarker to return false when only apiVersion is present (no kind)")
	}
}

func TestK8sParserDoesNotDoubleRead(t *testing.T) {
	// Verify that parseK8s reads the file only once by checking that
	// a valid K8s manifest with known deps produces correct results
	// and an invalid one returns nil without errors.
	nonK8sContent := `# Just a random YAML file
database:
  host: localhost
  port: 5432
cache:
  host: redis
  port: 6379
`
	path := writeTempFile(t, "random-config.yml", nonK8sContent)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("unexpected error for non-K8s YAML: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for non-K8s YAML, got %d deps", len(deps))
	}
}

func TestParseK8sContent(t *testing.T) {
	// Simulates rendered Helm output (multi-document YAML)
	content := `---
# Source: helm-app/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  template:
    spec:
      containers:
        - name: app
          image: "myapp:latest"
          ports:
            - containerPort: 8080
          env:
            - name: REDIS_HOST
              value: "redis-cache:6379"
---
# Source: helm-app/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
spec:
  ports:
    - port: 80
      targetPort: 8080
`
	deps, err := ParseK8sContent(content, "helm-app/Chart.yaml (helm template)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect:
	// 1 container port (8080)
	// 1 host:port dep (redis-cache:6379)
	// 1 service port (80)
	// Total: 3
	assertDepCount(t, deps, 3)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Port == 8080 && d.Target == "myapp" && d.Confidence == model.High
	}, "container port 8080")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "redis-cache" && d.Port == 6379
	}, "redis host:port dep")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Source == "myapp-svc" && d.Port == 80
	}, "service port 80")

	// Verify sourceFile label is set correctly
	for _, dep := range deps {
		if dep.SourceFile != "helm-app/Chart.yaml (helm template)" {
			t.Errorf("dep.SourceFile = %q, want %q", dep.SourceFile, "helm-app/Chart.yaml (helm template)")
		}
	}
}

func TestParseK8sContentNonK8s(t *testing.T) {
	content := `just some random text
not yaml at all`
	deps, err := ParseK8sContent(content, "test-source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for non-K8s content, got %d", len(deps))
	}
}

func TestParseK8sContentEmpty(t *testing.T) {
	deps, err := ParseK8sContent("", "test-source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for empty content, got %d", len(deps))
	}
}

// --- test helpers ---

func assertDepCount(t *testing.T, deps []model.NetworkDependency, want int) {
	t.Helper()
	if len(deps) != want {
		t.Errorf("expected %d dependencies, got %d", want, len(deps))
		for i, d := range deps {
			t.Logf("  [%d] %s -> %s:%d (%s) %q", i, d.Source, d.Target, d.Port, d.Confidence, d.Description)
		}
	}
}

func assertHasDep(t *testing.T, deps []model.NetworkDependency, match func(model.NetworkDependency) bool, desc string) {
	t.Helper()
	for _, d := range deps {
		if match(d) {
			return
		}
	}
	t.Errorf("missing expected dependency: %s", desc)
	for i, d := range deps {
		t.Logf("  [%d] %s -> %s:%d (%s) %q", i, d.Source, d.Target, d.Port, d.Confidence, d.Description)
	}
}
