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

func TestDiffCommandExitCodeRequiresLicense(t *testing.T) {
	resetLicenseState(t)
	fixtureDir := findFixtureDir(t)

	registry := parser.DefaultRegistry()
	ds, _, err := walker.Walk(fixtureDir, registry)
	if err != nil {
		t.Fatalf("walker.Walk failed: %v", err)
	}
	baselineJSON, _ := json.MarshalIndent(ds, "", "  ")
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselineFile, baselineJSON, 0644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	diffExitCode = true

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"diff", "--exit-code", baselineFile, fixtureDir})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected --exit-code without license to error")
	}
	if !strings.Contains(err.Error(), "Pro license") {
		t.Errorf("expected error to mention Pro license, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "exit-code") {
		t.Errorf("expected error to name --exit-code, got %q", err.Error())
	}
	resetLicenseState(t)
}

func TestDiffCommandExitCodeWithProLicense(t *testing.T) {
	resetLicenseState(t)
	if _, err := resolveLicenseHelper(t, "pro"); err != nil {
		t.Fatalf("setup license: %v", err)
	}
	fixtureDir := findFixtureDir(t)

	registry := parser.DefaultRegistry()
	ds, _, err := walker.Walk(fixtureDir, registry)
	if err != nil {
		t.Fatalf("walker.Walk failed: %v", err)
	}
	baselineJSON, _ := json.MarshalIndent(ds, "", "  ")
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselineFile, baselineJSON, 0644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	diffExitCode = true

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"diff", "--exit-code", baselineFile, fixtureDir})

	// Diffing the fixture against itself is a no-op; --exit-code with no
	// changes returns nil, not errChangesDetected.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected --exit-code with pro license + no changes to succeed, got %v", err)
	}
	resetLicenseState(t)
}

func TestDiffCommandIdentical(t *testing.T) {
	resetLicenseState(t)
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
	resetLicenseState(t)
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
