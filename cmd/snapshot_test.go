package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapshotWritesMetadataBlock verifies the new snapshot format includes
// a populated provenance metadata block — created_utc, segspec_version,
// input_path, plus git_commit and git_dirty fields.
func TestSnapshotWritesMetadataBlock(t *testing.T) {
	resetLicenseState(t)
	fixtureDir := findFixtureDir(t)

	outFile := filepath.Join(t.TempDir(), "baseline.json")
	outputFile = outFile
	defer func() { outputFile = "" }()

	cmd := rootCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"snapshot", fixtureDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("snapshot command failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	var snap struct {
		Metadata struct {
			CreatedUTC     string `json:"created_utc"`
			SegspecVersion string `json:"segspec_version"`
			GitCommit      string `json:"git_commit"`
			InputPath      string `json:"input_path"`
		} `json:"metadata"`
		Dependencies []map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("parse snapshot JSON: %v", err)
	}
	if snap.Metadata.CreatedUTC == "" {
		t.Errorf("expected metadata.created_utc to be populated")
	}
	if snap.Metadata.SegspecVersion != Version {
		t.Errorf("expected metadata.segspec_version=%q, got %q", Version, snap.Metadata.SegspecVersion)
	}
	if snap.Metadata.InputPath == "" {
		t.Errorf("expected metadata.input_path to be populated")
	}
	if snap.Metadata.GitCommit == "" {
		t.Errorf("expected metadata.git_commit to be populated (got empty string, should be SHA or 'unknown')")
	}
	if len(snap.Dependencies) == 0 {
		t.Errorf("expected snapshot to carry dependencies")
	}
}

// TestSnapshotGitCommitOutsideRepo verifies that running snapshot in a
// directory that's not under git control yields git_commit="unknown".
func TestSnapshotGitCommitOutsideRepo(t *testing.T) {
	resetLicenseState(t)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Build a non-git directory containing a fixture file that produces
	// at least one dependency (so the snapshot is meaningful).
	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, "docker-compose.yml"), []byte(`
services:
  web:
    image: nginx
    environment:
      - DATABASE_URL=postgresql://db:5432/app
  db:
    image: postgres
`), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outFile := filepath.Join(t.TempDir(), "baseline.json")
	outputFile = outFile
	defer func() { outputFile = "" }()

	cmd := rootCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"snapshot", scratch})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("snapshot command failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	var snap struct {
		Metadata SnapshotMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("parse snapshot JSON: %v", err)
	}
	if snap.Metadata.GitCommit != "unknown" {
		t.Errorf("expected git_commit=unknown outside a repo, got %q", snap.Metadata.GitCommit)
	}
	if snap.Metadata.GitDirty {
		t.Errorf("expected git_dirty=false outside a repo, got true")
	}
}

// TestDiffAcceptsSnapshotWithMetadata verifies that diffing a path against
// a snapshot file that carries a metadata block works exactly as it does
// against a legacy baseline — the metadata is informational only.
func TestDiffAcceptsSnapshotWithMetadata(t *testing.T) {
	resetLicenseState(t)
	fixtureDir := findFixtureDir(t)

	// Step 1: take a snapshot of the fixture.
	snapshotFilePath := filepath.Join(t.TempDir(), "baseline.json")
	outputFile = snapshotFilePath
	cmd := rootCmd
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"snapshot", fixtureDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	outputFile = ""

	// Sanity-check: the snapshot file actually has a metadata block.
	raw, err := os.ReadFile(snapshotFilePath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"metadata"`)) {
		t.Fatalf("expected snapshot to contain a metadata block, got:\n%s", raw)
	}

	// Step 2: diff against the same fixture — should produce "No changes".
	buf := new(bytes.Buffer)
	outputFormat = "summary"
	diffExitCode = false
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"diff", snapshotFilePath, fixtureDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No changes detected.") {
		t.Errorf("expected no-change diff against snapshot baseline, got:\n%s", buf.String())
	}
}

// TestDiffWarnsOnVersionMismatch verifies that when the baseline was created
// by a different segspec version than the running binary, a warning is
// emitted to stderr (but the diff still runs).
func TestDiffWarnsOnVersionMismatch(t *testing.T) {
	resetLicenseState(t)
	fixtureDir := findFixtureDir(t)

	// Hand-craft a snapshot with a deliberately-wrong segspec_version.
	// The dependency list is empty, so the diff will just report the fixture
	// deps as added — but our assertion is about the warning, not the diff.
	snap := snapshotFile{
		Metadata: SnapshotMetadata{
			CreatedUTC:     "2025-01-01T00:00:00Z",
			SegspecVersion: "0.0.1-stale",
			GitCommit:      "deadbeef",
			InputPath:      "/some/where",
		},
		Service:      "stale",
		Generated:    "2025-01-01",
		Version:      "0.0.1-stale",
		Dependencies: nil,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselineFile, data, 0644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	stderr := captureStderr(t, func() {
		buf := new(bytes.Buffer)
		outputFormat = "summary"
		diffExitCode = false
		cmd := rootCmd
		cmd.SetOut(buf)
		cmd.SetErr(buf) // cobra's err writer; warning still goes to os.Stderr
		cmd.SetArgs([]string{"diff", baselineFile, fixtureDir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("diff failed: %v", err)
		}
	})

	if !strings.Contains(stderr, "0.0.1-stale") {
		t.Errorf("expected warning to mention baseline version 0.0.1-stale, stderr was:\n%s", stderr)
	}
	if !strings.Contains(stderr, Version) {
		t.Errorf("expected warning to mention current version %s, stderr was:\n%s", Version, stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "warning") {
		t.Errorf("expected stderr to include a warning, got:\n%s", stderr)
	}
}

// TestDiffAcceptsLegacyBaselineSilently verifies backward compatibility: a
// baseline file without a metadata block (the v0.5.0 format) still works,
// and produces no version-mismatch warning.
func TestDiffAcceptsLegacyBaselineSilently(t *testing.T) {
	resetLicenseState(t)
	fixtureDir := findFixtureDir(t)

	// Build a legacy-shape baseline: bare DependencySet JSON, no metadata.
	legacy := map[string]any{
		"service":      "legacy",
		"generated":    "2024-01-01",
		"version":      "0.5.0",
		"dependencies": []any{},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy baseline: %v", err)
	}
	baselineFile := filepath.Join(t.TempDir(), "baseline-legacy.json")
	if err := os.WriteFile(baselineFile, data, 0644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	stderr := captureStderr(t, func() {
		buf := new(bytes.Buffer)
		outputFormat = "summary"
		diffExitCode = false
		cmd := rootCmd
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"diff", baselineFile, fixtureDir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("diff failed: %v", err)
		}
	})

	if strings.Contains(strings.ToLower(stderr), "baseline was created") {
		t.Errorf("expected no version-mismatch warning for legacy baseline, got stderr:\n%s", stderr)
	}
}

// captureStderr swaps os.Stderr for the duration of fn and returns whatever
// fn wrote to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	w.Close()
	return <-done
}
