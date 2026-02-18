package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeE2E_FullStack(t *testing.T) {
	// Find the testdata/fullstack fixture directory
	fixtureDir := findFixtureDir(t)

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:   "summary format shows dependencies",
			format: "summary",
			contains: []string{
				"Service:",
				"Dependencies:",
				"5432",
				"6379",
				"9092",
			},
		},
		{
			name:   "netpol format produces valid YAML",
			format: "netpol",
			contains: []string{
				"apiVersion: networking.k8s.io/v1",
				"kind: NetworkPolicy",
				"Egress",
			},
		},
		{
			name:   "all format includes both",
			format: "all",
			contains: []string{
				"Service:",
				"apiVersion: networking.k8s.io/v1",
				"---",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)

			// Reset global flags
			outputFormat = tt.format
			outputFile = ""

			cmd := rootCmd
			cmd.SetOut(buf)
			cmd.SetArgs([]string{"analyze", fixtureDir})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("analyze command failed: %v", err)
			}

			output := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\nGot:\n%s", want, output)
				}
			}
		})
	}
}

func TestAnalyzeE2E_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = ""

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"analyze", dir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("analyze command failed: %v", err)
	}

	if !strings.Contains(buf.String(), "No network dependencies found") {
		t.Errorf("expected 'No network dependencies found', got: %s", buf.String())
	}
}

func TestAnalyzeE2E_OutputFile(t *testing.T) {
	fixtureDir := findFixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "output.txt")

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = outFile

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"analyze", fixtureDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("analyze command failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !strings.Contains(string(data), "Service:") {
		t.Errorf("output file missing expected content, got: %s", string(data))
	}
}

func TestAnalyzeE2E_InvalidPath(t *testing.T) {
	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = ""

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"analyze", "/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestAnalyzeE2E_DetectsMultipleSourceTypes(t *testing.T) {
	fixtureDir := findFixtureDir(t)

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	outputFile = ""

	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"analyze", fixtureDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("analyze command failed: %v", err)
	}

	output := buf.String()

	// Should find deps from multiple parser types
	sourceTypes := []string{
		"PostgreSQL",   // from Spring config or compose well-known image
		"Redis",        // from Spring config or compose well-known image
		"Kafka",        // from Spring config or compose well-known image
	}
	found := 0
	for _, st := range sourceTypes {
		if strings.Contains(output, st) {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected deps from multiple source types, only found %d of %d\nOutput:\n%s", found, len(sourceTypes), output)
	}
}

func findFixtureDir(t *testing.T) string {
	t.Helper()
	// Try relative to module root
	candidates := []string{
		"internal/parser/testdata/fullstack",
		"../internal/parser/testdata/fullstack",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	t.Skip("fixture directory not found")
	return ""
}
