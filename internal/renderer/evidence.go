package renderer

import (
	"fmt"
	"strings"
	"time"

	"github.com/dormstern/segspec/internal/model"
)

// Evidence renders a Markdown evidence report explaining why each dependency exists.
func Evidence(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return "No dependencies found.\n"
	}

	var highCount, medCount, lowCount int
	for _, dep := range deps {
		switch dep.Confidence {
		case model.High:
			highCount++
		case model.Medium:
			medCount++
		case model.Low:
			lowCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Network Dependency Evidence Report\n")
	fmt.Fprintf(&b, "Generated: %s | Tool: segspec v0.5.0\n\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "## Summary\n")
	fmt.Fprintf(&b, "Total: %d | High: %d | Medium: %d | Low: %d\n\n", len(deps), highCount, medCount, lowCount)
	fmt.Fprintf(&b, "## Dependencies\n\n")

	for _, dep := range deps {
		source := dep.Source
		if source == "" {
			source = ds.ServiceName
		}

		confLabel := strings.ToUpper(string(dep.Confidence))
		marker := ""
		if dep.Confidence == model.Low {
			marker = " \u26a0"
		}

		fmt.Fprintf(&b, "### %s \u2192 %s:%d/%s [%s]%s\n", source, dep.Target, dep.Port, dep.Protocol, confLabel, marker)
		fmt.Fprintf(&b, "Justification: %s\n", dep.Description)
		fmt.Fprintf(&b, "Source: %s\n", dep.SourceFile)
		if dep.EvidenceLine != "" {
			fmt.Fprintf(&b, "Evidence: `%s`\n", model.RedactSecrets(dep.EvidenceLine))
		} else {
			fmt.Fprintf(&b, "Evidence: (no direct config line)\n")
		}
		fmt.Fprintf(&b, "\n")
	}

	return b.String()
}
