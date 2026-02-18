package walker

import (
	"os"
	"strings"
	"testing"
)

func TestRenderHelmTemplate(t *testing.T) {
	// Test with nonexistent chart — should return error gracefully
	output, err := renderHelmTemplate("/nonexistent/chart", "")
	if err == nil {
		t.Fatal("expected error for nonexistent chart")
	}
	if output != "" {
		t.Errorf("expected empty output on error, got %q", output)
	}
}

func TestRenderHelmTemplate_NotInstalled(t *testing.T) {
	// Save PATH and set to empty to simulate helm not found
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	_, err := renderHelmTemplate("testdata/helm-app", "")
	if err == nil {
		t.Fatal("expected error when helm is not installed")
	}
	if !strings.Contains(err.Error(), "helm") {
		t.Errorf("error should mention helm: %v", err)
	}
}
