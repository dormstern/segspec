package walker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dormorgenstern/segspec/internal/model"
	"github.com/dormorgenstern/segspec/internal/parser"
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

	ds, err := Walk(dir, r)
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

	ds, err := Walk(dir, r)
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

	ds, err := Walk(dir, r)
	if err != nil {
		t.Fatalf("Walk() error: %v", err)
	}
	if ds.Len() != 0 {
		t.Errorf("Len() = %d for empty dir, want 0", ds.Len())
	}
}
