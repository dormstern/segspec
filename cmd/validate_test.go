package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_EmptyDirSucceeds verifies the wave-2 contract that an empty
// input directory exits 0 with a "no policies found" stderr hint. Caller
// pipelines (CI greps) must not see findings on stdout.
func TestValidate_EmptyDirSucceeds(t *testing.T) {
	resetLicenseState(t)
	dir := t.TempDir()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"validate", dir})
	defer func() { validateJSON = false }()

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected nil error on empty dir, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on empty dir, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no policies found") {
		t.Errorf("expected 'no policies found' on stderr, got %q", stderr.String())
	}
}

// TestValidate_BadPolicyExitsNonZero verifies that an oversized-label
// finding (severity=error) propagates the errChangesDetected sentinel,
// which Execute() translates to exit code 1. CI scripts depend on this.
func TestValidate_BadPolicyExitsNonZero(t *testing.T) {
	resetLicenseState(t)
	dir := t.TempDir()
	long := strings.Repeat("z", 80)
	body := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: bad
spec:
  podSelector:
    matchLabels:
      app: ` + long + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"validate", dir})
	defer func() { validateJSON = false }()

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error to drive exit 1, got nil")
	}
	if !errors.Is(err, errChangesDetected) {
		t.Errorf("expected errChangesDetected, got %v", err)
	}
	if !strings.Contains(stdout.String(), "oversized-selector-label") {
		t.Errorf("expected oversized-selector-label finding on stdout, got %q", stdout.String())
	}
}
