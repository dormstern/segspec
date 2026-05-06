package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComposeDisableFull verifies that a `# segspec:disable=full` annotation
// on a Compose service marks every dep emitted for that service as
// fully-disabled, while siblings remain untouched. Driver: k8s upstream
// #112560 — "disable the networkpolicy temporarily ... without delete or
// edit to match none."
func TestComposeDisableFull(t *testing.T) {
	dir := t.TempDir()
	content := `services:
  # segspec:disable=full
  web:
    image: nginx
    ports:
      - "8080:80"
    depends_on:
      - db
  db:
    image: postgres:15
`
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps, err := parseCompose(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Every dep where Source=="web" should carry Disabled=="full".
	var webDeps, dbDeps int
	for _, d := range deps {
		if d.Source == "web" {
			webDeps++
			if d.Disabled != "full" {
				t.Errorf("web dep %v: Disabled = %q, want \"full\"", d, d.Disabled)
			}
		}
		if d.Source == "db" || (d.Source == "" && d.Target == "db") {
			dbDeps++
			if d.Disabled != "" {
				t.Errorf("db dep %v: Disabled = %q, want empty", d, d.Disabled)
			}
		}
	}
	if webDeps == 0 {
		t.Fatal("expected at least one web dep")
	}
}

// TestK8sDisableLabelIngress verifies that a `segspec.io/disable: ingress`
// label on a Deployment marks deps for that workload as ingress-disabled
// while leaving egress deps alone.
func TestK8sDisableLabelIngress(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-service
  labels:
    segspec.io/disable: ingress
spec:
  template:
    spec:
      containers:
      - name: order
        image: order:latest
        ports:
        - containerPort: 8080
        env:
        - name: DATABASE_URL
          value: "postgresql://db.prod.svc.cluster.local:5432/orders"
`
	path := writeTempFile(t, "deployment.yaml", manifest)
	deps, err := parseK8s(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(deps) == 0 {
		t.Fatal("expected deps")
	}
	for _, d := range deps {
		if d.Source != "order-service" && d.Target != "order-service" {
			continue
		}
		if d.Disabled != "ingress" {
			t.Errorf("dep %+v: Disabled = %q, want \"ingress\"", d, d.Disabled)
		}
	}
}

// TestSpringDisableEgress verifies a `# segspec:disable=egress` comment in
// application.yml propagates to every emitted dep.
func TestSpringDisableEgress(t *testing.T) {
	dir := t.TempDir()
	content := `# segspec:disable=egress
spring:
  datasource:
    url: jdbc:postgresql://db-host:5432/mydb
  redis:
    host: redis-host
    port: 6379
`
	path := filepath.Join(dir, "application.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps, err := parseSpringYAML(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(deps) == 0 {
		t.Fatal("expected deps")
	}
	for _, d := range deps {
		if d.Disabled != "egress" {
			t.Errorf("dep %+v: Disabled = %q, want \"egress\"", d, d.Disabled)
		}
	}
}

// TestParseDisableDirective_Values exercises the shared comment parser used
// by every format-specific parser.
func TestParseDisableDirective_Values(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"# segspec:disable=full", "full"},
		{"#segspec:disable=ingress", "ingress"},
		{"   # segspec:disable=egress  ", "egress"},
		{"# segspec:disable=bogus", ""},
		{"# unrelated comment", ""},
		{"segspec:disable=full", "full"},
		{"segspec.io/disable: \"ingress\"", "ingress"},
		{"", ""},
	}
	for _, c := range cases {
		got := ParseDisableDirective(c.in)
		if got != c.want {
			t.Errorf("ParseDisableDirective(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
