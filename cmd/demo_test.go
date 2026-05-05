package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestAnalyzeDemo_List asserts that `--demo list` prints every catalogued
// demo's name and its one-line description to stdout. This is the
// discoverability surface — if it breaks, users can't find the fixtures.
func TestAnalyzeDemo_List(t *testing.T) {
	resetLicenseState(t)
	resetAnalyzeFlags(t)

	buf := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"analyze", "--demo", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--demo list returned error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"sentry-mini", "microservices-demo", "Sentry", "microservices"} {
		if !strings.Contains(out, want) {
			t.Errorf("--demo list output missing %q\nGot:\n%s", want, out)
		}
	}
}

// TestAnalyzeDemo_SentryMini exercises the happy path: pointing segspec at
// the embedded sentry-mini fixture should yield non-empty dependency
// output. We don't assert exact dep counts (parser surface drift would
// thrash this); we just confirm the embed-extract-walk pipeline works
// end-to-end and produces recognisable summary content.
func TestAnalyzeDemo_SentryMini(t *testing.T) {
	resetLicenseState(t)
	resetAnalyzeFlags(t)

	buf := new(bytes.Buffer)
	outputFormat = "summary"
	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"analyze", "--demo", "sentry-mini"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--demo sentry-mini returned error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "No network dependencies found") {
		t.Fatalf("expected non-empty deps for sentry-mini, got empty output:\n%s", out)
	}
	// Summary always names the service it analysed; the temp-dir name is
	// randomised, but the output should still mention "Service:" and at
	// least one well-known port from the fixture (5432 = postgres,
	// 6379 = redis, 9092 = kafka).
	for _, want := range []string{"Service:", "Dependencies:"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q\nGot:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "5432") && !strings.Contains(out, "6379") && !strings.Contains(out, "9092") {
		t.Errorf("expected at least one of the well-known ports (5432/6379/9092) in summary, got:\n%s", out)
	}
}

// TestAnalyzeDemo_Unknown asserts a clear error path: an unknown demo
// name must exit non-zero and the error must list the valid demo names
// so the user can self-correct without scrolling for help text.
func TestAnalyzeDemo_Unknown(t *testing.T) {
	resetLicenseState(t)
	resetAnalyzeFlags(t)

	buf := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"analyze", "--demo", "definitely-not-a-demo"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --demo with unknown name to error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sentry-mini") || !strings.Contains(msg, "microservices-demo") {
		t.Errorf("error should list valid demo names; got %q", msg)
	}
}

// TestAnalyzeDemo_MutuallyExclusiveWithPath enforces that --demo and a
// positional path argument are not both accepted at once. Letting both
// through would silently ignore one — the spec calls this out explicitly.
func TestAnalyzeDemo_MutuallyExclusiveWithPath(t *testing.T) {
	resetLicenseState(t)
	resetAnalyzeFlags(t)

	buf := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"analyze", "--demo", "sentry-mini", t.TempDir()})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --demo + positional path to error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "demo") {
		t.Errorf("mutual-exclusion error should mention --demo; got %q", err.Error())
	}
}

// resetAnalyzeFlags clears the package-level flag globals that cobra
// holds onto between test runs so a stale value from a prior test
// doesn't leak into the next. Mirrors the resetLicenseState helper.
func resetAnalyzeFlags(t *testing.T) {
	t.Helper()
	prevDemo := demoName
	prevFormat := outputFormat
	prevFile := outputFile
	prevAI := aiProvider
	prevHelm := helmValuesFile
	demoName = ""
	outputFormat = "summary"
	outputFile = ""
	aiProvider = ""
	helmValuesFile = ""
	t.Cleanup(func() {
		demoName = prevDemo
		outputFormat = prevFormat
		outputFile = prevFile
		aiProvider = prevAI
		helmValuesFile = prevHelm
	})
}
