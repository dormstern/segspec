package walker

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// renderHelmTemplate shells out to `helm template` to render a chart.
// valuesFile is optional — if empty, uses the chart's default values.yaml.
// Returns the rendered YAML as a string.
func renderHelmTemplate(chartDir string, valuesFile string) (string, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return "", fmt.Errorf("helm not installed: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"template", "segspec-render", chartDir}
	if valuesFile != "" {
		args = append(args, "-f", valuesFile)
	}

	cmd := exec.CommandContext(ctx, "helm", args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("helm template timed out after 30s: %w", err)
		}
		return "", fmt.Errorf("helm template failed: %w", err)
	}

	return string(out), nil
}
