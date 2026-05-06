package renderer

import (
	"fmt"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

// Summary renders a human-readable dependency report.
func Summary(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return "No dependencies found.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Service: %s\n", ds.ServiceName)
	fmt.Fprintf(&b, "Dependencies: %d\n\n", len(deps))

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

		conf := string(dep.Confidence)
		desc := dep.Description
		if desc == "" {
			desc = fmt.Sprintf("%s:%d/%s", dep.Target, dep.Port, dep.Protocol)
		}
		// Surface the source-level disable directive so operators can SEE
		// that a workload's policies are intentionally suppressed (k8s
		// upstream #112560 — "disable temporarily without delete-or-edit").
		// We render the marker even though the netpol formats skip the
		// rule; making suppression invisible would defeat the auditability
		// principle that drives the evidence-bundle format.
		disabledTag := ""
		if dep.Disabled != "" {
			disabledTag = fmt.Sprintf("  [disabled: %s]", dep.Disabled)
		}
		fmt.Fprintf(&b, "  → %s:%d/%s  [%s]  %s%s\n", dep.Target, dep.Port, dep.Protocol, conf, desc, disabledTag)
		if dep.SourceFile != "" {
			fmt.Fprintf(&b, "    source: %s\n", dep.SourceFile)
		}
	}

	fmt.Fprintf(&b, "\nConfidence: %d high, %d medium, %d low\n", highCount, medCount, lowCount)
	if lowCount > 0 {
		fmt.Fprintf(&b, "⚠ %d low-confidence dependencies — verify before enforcing\n", lowCount)
	}

	return b.String()
}
