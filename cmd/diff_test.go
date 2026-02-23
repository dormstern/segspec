package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/walker"
)

func TestDiffCommandIdentical(t *testing.T) {
	fixtureDir := findFixtureDir(t)

	// First, analyze the fixture directory to produce a baseline.
	registry := parser.DefaultRegistry()
	ds, _, err := walker.Walk(fixtureDir, registry)
	if err != nil {
		t.Fatalf("walker.Walk failed: %v", err)
	}

	baselineJSON, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Write baseline to a temp file.
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselineFile, baselineJSON, 0644); err != nil {
		t.Fatalf("failed to write baseline: %v", err)
	}

	// Run diff against the same directory — should show no changes.
	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = ""
	diffExitCode = false

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"diff", baselineFile, fixtureDir})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("diff command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No changes detected.") {
		t.Errorf("expected 'No changes detected.', got:\n%s", output)
	}
}

func TestDiffCommandDetectsChanges(t *testing.T) {
	fixtureDir := findFixtureDir(t)

	// Create a minimal baseline with a dep that won't be in the fixture.
	baseline := model.NewDependencySet("test")
	baseline.Add(model.NetworkDependency{
		Source: "ghost", Target: "nonexistent-db", Port: 9999, Protocol: "TCP",
		Confidence: model.High, SourceFile: "old-config.yml",
	})

	baselineJSON, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselineFile, baselineJSON, 0644); err != nil {
		t.Fatalf("failed to write baseline: %v", err)
	}

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = ""
	diffExitCode = false

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"diff", baselineFile, fixtureDir})

	err = cmd.Execute()
	if err != nil {
		t.Fatalf("diff command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "REMOVED") {
		t.Errorf("expected REMOVED section (baseline dep not in fixture), got:\n%s", output)
	}
	if !strings.Contains(output, "ADDED") {
		t.Errorf("expected ADDED section (fixture deps not in baseline), got:\n%s", output)
	}
}
