package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetCoverageState clears the coverage subcommand's package-level flag
// state. Cobra reuses the same flag values across Execute() calls, so each
// test must reset to avoid cross-contamination.
func resetCoverageState(t *testing.T) {
	t.Helper()
	resetLicenseState(t)
	coverageJSON = false
	coverageExitCode = false
	coverageThreshold = 100
}

// runCoverageCmd is a tiny helper that invokes `segspec coverage <args>` via
// the real cobra root and captures stdout/stderr. Mirrors the harness in
// validate_test.go.
func runCoverageCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SilenceErrors = false
	cmd.SetArgs(append([]string{"coverage"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// writeYAML drops a fixture into dir/name and fails the test if the write
// fails. Keeps each test body focused on the assertions.
func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestCoverage_SingleWorkloadCovered: one Deployment with one matching
// NetworkPolicy → 100% coverage, 0 orphans, exit 0.
func TestCoverage_SingleWorkloadCovered(t *testing.T) {
	resetCoverageState(t)
	dir := t.TempDir()

	writeYAML(t, dir, "deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: c
        image: nginx
`)
	writeYAML(t, dir, "policy.yaml", `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web-allow
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: web
`)

	stdout, _, err := runCoverageCmd(t, dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout, "covered") {
		t.Errorf("expected 'covered' line in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "1/1 workloads (100%)") {
		t.Errorf("expected '1/1 workloads (100%%)' summary, got %q", stdout)
	}
	if strings.Contains(stdout, "uncovered") {
		t.Errorf("did not expect any 'uncovered' lines, got %q", stdout)
	}
	if strings.Contains(stdout, "orphan") {
		t.Errorf("did not expect any 'orphan' lines, got %q", stdout)
	}
}

// TestCoverage_SingleWorkloadUncovered: one Deployment, no NetworkPolicy
// → 0% coverage. Exit 0 because --exit-code is not set; the report alone
// is informational.
func TestCoverage_SingleWorkloadUncovered(t *testing.T) {
	resetCoverageState(t)
	dir := t.TempDir()

	writeYAML(t, dir, "deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
      - name: c
        image: nginx
`)

	stdout, _, err := runCoverageCmd(t, dir)
	if err != nil {
		t.Fatalf("expected nil error without --exit-code, got %v", err)
	}
	if !strings.Contains(stdout, "uncovered") {
		t.Errorf("expected 'uncovered' marker, got %q", stdout)
	}
	if !strings.Contains(stdout, "0/1 workloads (0%)") {
		t.Errorf("expected '0/1 workloads (0%%)' summary, got %q", stdout)
	}
}

// TestCoverage_MixedThreeOfFourCovered: 4 K8s workloads, only 3 have a
// matching policy → exact 75% coverage.
func TestCoverage_MixedThreeOfFourCovered(t *testing.T) {
	resetCoverageState(t)
	dir := t.TempDir()

	for _, name := range []string{"web", "api", "worker", "lonely"} {
		writeYAML(t, dir, name+".yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: `+name+`
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: `+name+`
    spec:
      containers:
      - name: c
        image: nginx
`)
	}
	// Three policies covering web, api, worker. lonely intentionally has
	// no matching policy.
	for _, name := range []string{"web", "api", "worker"} {
		writeYAML(t, dir, "p-"+name+".yaml", `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: `+name+`-allow
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: `+name+`
`)
	}

	stdout, _, err := runCoverageCmd(t, dir, "--json")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	var rep struct {
		TotalWorkloads   int `json:"total_workloads"`
		CoveredWorkloads int `json:"covered_workloads"`
		Percent          int `json:"coverage_percent"`
		OrphanPolicies   []struct {
			Name string `json:"name"`
		} `json:"orphan_policies"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, stdout)
	}
	if rep.TotalWorkloads != 4 {
		t.Errorf("total_workloads: want 4, got %d", rep.TotalWorkloads)
	}
	if rep.CoveredWorkloads != 3 {
		t.Errorf("covered_workloads: want 3, got %d", rep.CoveredWorkloads)
	}
	if rep.Percent != 75 {
		t.Errorf("coverage_percent: want 75, got %d", rep.Percent)
	}
	if len(rep.OrphanPolicies) != 0 {
		t.Errorf("expected 0 orphans, got %+v", rep.OrphanPolicies)
	}
}

// TestCoverage_OrphanPolicyReported: a policy whose podSelector matches no
// workload in the input set is listed in the orphans block and counted as
// such.
func TestCoverage_OrphanPolicyReported(t *testing.T) {
	resetCoverageState(t)
	dir := t.TempDir()

	writeYAML(t, dir, "deploy.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: c
        image: nginx
`)
	// Two policies: one matches `web`, one matches a workload that doesn't
	// exist (typo). The second is the orphan.
	writeYAML(t, dir, "p-web.yaml", `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web-allow
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: web
`)
	writeYAML(t, dir, "p-stale.yaml", `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stale-typo
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: web-old-name
`)

	stdout, _, err := runCoverageCmd(t, dir, "--json")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	var rep struct {
		Percent        int `json:"coverage_percent"`
		OrphanPolicies []struct {
			Name string `json:"name"`
		} `json:"orphan_policies"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if rep.Percent != 100 {
		t.Errorf("coverage_percent: want 100 (web is covered), got %d", rep.Percent)
	}
	if len(rep.OrphanPolicies) != 1 || rep.OrphanPolicies[0].Name != "stale-typo" {
		t.Errorf("expected one orphan named 'stale-typo', got %+v", rep.OrphanPolicies)
	}
}

// TestCoverage_ThresholdGateExitNonZero: with --exit-code + --threshold 80
// against a 75%-covered input set, the command must exit non-zero. The
// gate is a paid feature so we mint a Pro license first.
func TestCoverage_ThresholdGateExitNonZero(t *testing.T) {
	resetCoverageState(t)

	// Pro license unlocks --exit-code (mirrors diff --exit-code gating).
	licenseKey = mintTestToken(t, "pro", time.Now().Add(24*time.Hour))
	if err := resolveLicense(nil, nil); err != nil {
		t.Fatalf("resolveLicense: %v", err)
	}

	dir := t.TempDir()
	for _, name := range []string{"web", "api", "worker", "lonely"} {
		writeYAML(t, dir, name+".yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: `+name+`
  namespace: default
spec:
  template:
    metadata:
      labels:
        app: `+name+`
    spec:
      containers:
      - name: c
        image: nginx
`)
	}
	for _, name := range []string{"web", "api", "worker"} {
		writeYAML(t, dir, "p-"+name+".yaml", `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: `+name+`-allow
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: `+name+`
`)
	}

	_, _, err := runCoverageCmd(t, dir, "--exit-code", "--threshold", "80")
	if err == nil {
		t.Fatal("expected non-nil error from --exit-code with 75% < 80% threshold, got nil")
	}
	if !errors.Is(err, errChangesDetected) {
		t.Errorf("expected errChangesDetected sentinel, got %v", err)
	}
}

// TestCoverage_EmptyDirGracefulSuccess: empty input directory exits 0 with
// a 'no workloads found' hint on stderr. Stdout stays clean so CI greps
// for findings see nothing — this mirrors `validate`'s empty-dir contract.
func TestCoverage_EmptyDirGracefulSuccess(t *testing.T) {
	resetCoverageState(t)
	dir := t.TempDir()

	stdout, stderr, err := runCoverageCmd(t, dir)
	if err != nil {
		t.Fatalf("expected nil error on empty dir, got %v", err)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout on empty dir, got %q", stdout)
	}
	if !strings.Contains(stderr, "no workloads found") {
		t.Errorf("expected 'no workloads found' on stderr, got %q", stderr)
	}
}
