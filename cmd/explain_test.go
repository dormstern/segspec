package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExplain_CmdJSONOutput is the cmd-level integration smoke test:
// writes a real NetworkPolicy YAML, invokes the cobra command via rootCmd,
// and verifies the JSON output structure end-to-end. This is the only cmd
// test for explain; the renderer + explainer packages cover behavior.
func TestExplain_CmdJSONOutput(t *testing.T) {
	resetLicenseState(t)
	dir := t.TempDir()
	body := `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-allow
  namespace: prod
spec:
  podSelector:
    matchLabels:
      app: api
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
`
	if err := os.WriteFile(filepath.Join(dir, "p.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd := rootCmd
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"explain", "api", "--policies", dir, "--labels", "app=api", "--namespace", "prod", "--json"})
	t.Cleanup(func() {
		explainPoliciesPath = ""
		explainLabels = ""
		explainNamespace = ""
		explainJSON = false
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("explain failed: %v (stderr=%s)", err, stderr.String())
	}
	out := stdout.String()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if _, ok := got["effective_ingress"]; !ok {
		t.Errorf("expected effective_ingress key in cmd JSON, got: %v", got)
	}
	pols, _ := got["policies"].([]any)
	if len(pols) != 1 {
		t.Fatalf("expected 1 applied policy, got %v", got["policies"])
	}
	if !strings.Contains(out, "api-allow") {
		t.Errorf("expected applied-policy name in output, got %q", out)
	}
}
