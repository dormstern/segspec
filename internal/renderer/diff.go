package renderer

import (
	"fmt"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

// Diff renders a human-readable diff report showing added, removed, and unchanged dependencies.
func Diff(d model.DependencyDiff) string {
	if len(d.Added) == 0 && len(d.Removed) == 0 {
		return "No changes detected.\n"
	}

	var b strings.Builder
	fmt.Fprintln(&b, "Network Dependency Changes")
	fmt.Fprintln(&b, "==========================")
	fmt.Fprintln(&b)

	if len(d.Added) > 0 {
		fmt.Fprintf(&b, "ADDED (%d):\n", len(d.Added))
		for _, dep := range d.Added {
			source := dep.Source
			if source == "" {
				source = "unknown"
			}
			fmt.Fprintf(&b, "  + %s -> %s:%d/%s [%s]\n", source, dep.Target, dep.Port, dep.Protocol, dep.Confidence)
			if dep.EvidenceLine != "" {
				fmt.Fprintf(&b, "    Evidence: %s\n", model.RedactSecrets(dep.EvidenceLine))
			}
		}
		fmt.Fprintln(&b)
	}

	if len(d.Removed) > 0 {
		fmt.Fprintf(&b, "REMOVED (%d):\n", len(d.Removed))
		for _, dep := range d.Removed {
			source := dep.Source
			if source == "" {
				source = "unknown"
			}
			fmt.Fprintf(&b, "  - %s -> %s:%d/%s [%s]\n", source, dep.Target, dep.Port, dep.Protocol, dep.Confidence)
			if dep.SourceFile != "" {
				fmt.Fprintf(&b, "    Was in: %s\n", dep.SourceFile)
			}
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "UNCHANGED: %d dependencies\n", len(d.Unchanged))

	return b.String()
}
