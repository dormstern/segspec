package walker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
)

func TestWalkFindsMatchingFiles(t *testing.T) {
	// Create temp directory with test files
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "application.yml"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644)

	parsed := false
	r := parser.NewRegistry()
	r.Register("application.yml", func(path string) ([]model.NetworkDependency, error) {
		parsed = true
		return []model.NetworkDependency{
			{Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: model.High},
		}, nil
	})

	ds, _, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	if !parsed {
		t.Error("parser was not called for application.yml")
	}
	if ds.Len() != 1 {
		t.Errorf("Len() = %d, want 1", ds.Len())
	}
}

func TestWalkSetsSourceFromDirName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.env"), []byte("test"), 0644)

	r := parser.NewRegistry()
	r.Register("*.env", func(path string) ([]model.NetworkDependency, error) {
		return []model.NetworkDependency{
			{Target: "redis", Port: 6379, Protocol: "TCP"},
		}, nil
	})

	ds, _, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}

	deps := ds.Dependencies()
	if len(deps) == 0 {
		t.Fatal("no dependencies found")
	}
	if deps[0].Source != ds.ServiceName {
		t.Errorf("Source = %q, want %q", deps[0].Source, ds.ServiceName)
	}
}

func TestWalkSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "application.yml"), []byte("test"), 0644)

	called := false
	r := parser.NewRegistry()
	r.Register("application.yml", func(path string) ([]model.NetworkDependency, error) {
		called = true
		return nil, nil
	})

	Walk(dir, r)
	if called {
		t.Error("parser was called for file inside .git directory")
	}
}

func TestWalkSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules")
	os.MkdirAll(nmDir, 0755)
	os.WriteFile(filepath.Join(nmDir, "application.yml"), []byte("test"), 0644)

	called := false
	r := parser.NewRegistry()
	r.Register("application.yml", func(path string) ([]model.NetworkDependency, error) {
		called = true
		return nil, nil
	})

	Walk(dir, r)
	if called {
		t.Error("parser was called for file inside node_modules")
	}
}

func TestWalkEmptyDir(t *testing.T) {
	dir := t.TempDir()

	r := parser.NewRegistry()
	r.Register("*.yml", func(path string) ([]model.NetworkDependency, error) {
		return nil, nil
	})

	ds, warnings, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	if ds.Len() != 0 {
		t.Errorf("Len() = %d for empty dir, want 0", ds.Len())
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty dir, got %d", len(warnings))
	}
}

func TestWalkReturnsWarningsForParseErrors(t *testing.T) {
	dir := t.TempDir()

	// Write a "malformed" YAML file that the parser will fail on.
	os.WriteFile(filepath.Join(dir, "bad.yml"), []byte("{{{{invalid yaml"), 0644)

	// Also write a good file that succeeds.
	os.WriteFile(filepath.Join(dir, "good.yml"), []byte("valid: true"), 0644)

	r := parser.NewRegistry()
	r.Register("*.yml", func(path string) ([]model.NetworkDependency, error) {
		if filepath.Base(path) == "bad.yml" {
			return nil, fmt.Errorf("YAML parse error: invalid syntax")
		}
		return []model.NetworkDependency{
			{Target: "redis", Port: 6379, Protocol: "TCP", Confidence: model.High},
		}, nil
	})

	ds, warnings, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() fatal error: %v", err)
	}

	// The good file should still produce results.
	if ds.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (good.yml should still parse)", ds.Len())
	}

	// We should get exactly 1 warning for bad.yml.
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0].File != "bad.yml" {
		t.Errorf("warning File = %q, want %q", warnings[0].File, "bad.yml")
	}
	if warnings[0].Err == nil {
		t.Error("warning Err should not be nil")
	}
}

func TestDetectHelmCharts(t *testing.T) {
	charts := detectHelmCharts("testdata/helm-app")
	if len(charts) != 1 {
		t.Fatalf("got %d charts, want 1", len(charts))
	}
	if charts[0] != "testdata/helm-app" {
		t.Errorf("chart path = %q, want %q", charts[0], "testdata/helm-app")
	}
}

func TestWalkHelmChartWarningWhenNoHelm(t *testing.T) {
	// When helm is not installed, Walk should produce a warning for Helm charts
	// Save PATH and set to empty to simulate helm not found
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	r := parser.NewRegistry()
	ds, warnings, err := Walk("testdata/helm-app", r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	// Should not crash even without helm
	_ = ds

	// Should have a warning about helm not being installed
	foundHelmWarning := false
	for _, w := range warnings {
		if strings.Contains(w.Err.Error(), "helm") {
			foundHelmWarning = true
			break
		}
	}
	if !foundHelmWarning {
		t.Errorf("expected warning about helm not installed, got %d warnings: %v", len(warnings), warnings)
	}
}

func TestWalkOptionsPassesHelmValues(t *testing.T) {
	// When WalkOptions.HelmValuesFile is set, it should be passed to renderHelmTemplate.
	// We test this indirectly: with helm not installed and a values file set,
	// the warning should still mention helm (not a values-file error).
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	r := parser.NewRegistry()
	opts := WalkOptions{HelmValuesFile: "testdata/helm-app/values.yaml"}
	ds, warnings, err := Walk("testdata/helm-app", r, opts)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	_ = ds

	foundHelmWarning := false
	for _, w := range warnings {
		if strings.Contains(w.Err.Error(), "helm") {
			foundHelmWarning = true
			break
		}
	}
	if !foundHelmWarning {
		t.Errorf("expected warning about helm, got %d warnings: %v", len(warnings), warnings)
	}
}

func TestDetectHelmChartsNone(t *testing.T) {
	dir := t.TempDir()
	charts := detectHelmCharts(dir)
	if len(charts) != 0 {
		t.Fatalf("got %d charts, want 0", len(charts))
	}
}

func TestWalkNoWarningsWhenAllFilesParse(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yml"), []byte("test"), 0644)

	r := parser.NewRegistry()
	r.Register("*.yml", func(path string) ([]model.NetworkDependency, error) {
		return []model.NetworkDependency{
			{Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: model.High},
		}, nil
	})

	_, warnings, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}
