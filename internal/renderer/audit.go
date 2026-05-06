package renderer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dormstern/segspec/internal/model"
)

// Audit renders an auditor-ready Markdown ledger that groups every discovered
// dependency by source workload, separates ingress from egress, anchors each
// row to its file:line evidence, and emits an explicit sign-off checklist
// mapped to the controls cluster auditors typically demand (one
// NetworkPolicy per workload, deny-by-default, justified east/west traffic).
//
// This output is intentionally distinct from the Evidence renderer: Evidence
// is a flat per-dependency list; Audit is a workload-scoped review document
// designed to be attached to a change-management ticket or PR review.
//
// Driver: cilium/cilium #43502 / #43503 — auditors require a defined
// NetworkPolicy on every workload; this output makes the evidence trail
// explicit and signoff-ready.
func Audit(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return "No dependencies found.\n"
	}

	var b strings.Builder
	now := time.Now().UTC().Format("2006-01-02")

	// --- Header + run fingerprint -----------------------------------------
	fmt.Fprintf(&b, "# Network Dependency Audit Ledger\n")
	fmt.Fprintf(&b, "Service: `%s`\n", ds.ServiceName)
	fmt.Fprintf(&b, "Generated: %s | Tool: segspec v0.6.0-dev\n", now)
	fmt.Fprintf(&b, "Run fingerprint: `%s`\n\n", auditFingerprint(ds))

	// --- Counters ---------------------------------------------------------
	highCount, medCount, lowCount := tallyConfidence(deps)
	workloads := uniqueWorkloads(deps)
	noEvidence := countMissingEvidence(deps)

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n")
	fmt.Fprintf(&b, "|---|---:|\n")
	fmt.Fprintf(&b, "| Workloads with declared traffic | %d |\n", len(workloads))
	fmt.Fprintf(&b, "| Total dependencies | %d |\n", len(deps))
	fmt.Fprintf(&b, "| High confidence (auto-approve candidates) | %d |\n", highCount)
	fmt.Fprintf(&b, "| Medium confidence (review) | %d |\n", medCount)
	fmt.Fprintf(&b, "| Low confidence (investigate) | %d |\n", lowCount)
	fmt.Fprintf(&b, "| Rows without direct evidence line | %d |\n", noEvidence)
	fmt.Fprintf(&b, "\n")

	// --- Per-workload sections --------------------------------------------
	fmt.Fprintf(&b, "## Workload sign-off\n\n")
	for _, w := range workloads {
		writeWorkloadSection(&b, ds, w)
	}

	// --- Auditor checklist ------------------------------------------------
	fmt.Fprintf(&b, "## Auditor checklist\n\n")
	fmt.Fprintf(&b, "- [ ] Every workload above has a corresponding `NetworkPolicy` resource (deny-by-default).\n")
	fmt.Fprintf(&b, "- [ ] Every HIGH-confidence egress is reflected in the generated policy.\n")
	if medCount > 0 {
		fmt.Fprintf(&b, "- [ ] Each of the %d MEDIUM-confidence row(s) has been reviewed by the service owner.\n", medCount)
	}
	if lowCount > 0 {
		fmt.Fprintf(&b, "- [ ] Each of the %d LOW-confidence row(s) has been confirmed or removed before enforcement.\n", lowCount)
	}
	if noEvidence > 0 {
		fmt.Fprintf(&b, "- [ ] %d row(s) without a direct config line have been traced to their actual source.\n", noEvidence)
	}
	fmt.Fprintf(&b, "- [ ] No row points at an external host that is not on the approved egress allow-list.\n")
	fmt.Fprintf(&b, "- [ ] This ledger has been attached to the change-management record.\n\n")

	fmt.Fprintf(&b, "_Generated deterministically from source. Re-running on the same inputs reproduces the same fingerprint._\n")

	return b.String()
}

// writeWorkloadSection emits one Markdown section for the named workload,
// with two tables: ingress (others -> workload) and egress (workload -> others).
func writeWorkloadSection(b *strings.Builder, ds *model.DependencySet, workload string) {
	ingress := ds.IngressFor(workload)
	egress := ds.EgressFor(workload)
	// Filter ingress to exclude self-loops; those are captured as "exposes" rows
	// in the egress table to avoid double-counting.
	ingressExt := make([]model.NetworkDependency, 0, len(ingress))
	for _, d := range ingress {
		if d.Source != workload {
			ingressExt = append(ingressExt, d)
		}
	}

	fmt.Fprintf(b, "### `%s`\n\n", workload)
	fmt.Fprintf(b, "Status: %s\n\n", workloadStatus(ingressExt, egress))

	// --- Egress table -----------------------------------------------------
	fmt.Fprintf(b, "**Egress** (this workload connects out to):\n\n")
	if len(egress) == 0 {
		fmt.Fprintf(b, "_No egress dependencies discovered._\n\n")
	} else {
		writeAuditTable(b, egress, "Target")
	}

	// --- Ingress table ----------------------------------------------------
	fmt.Fprintf(b, "**Ingress** (other workloads connecting in):\n\n")
	if len(ingressExt) == 0 {
		fmt.Fprintf(b, "_No external ingress declared._\n\n")
	} else {
		writeAuditTable(b, ingressExt, "Source")
	}
}

// writeAuditTable emits a Markdown table over the given slice of deps.
// peerLabel selects whether the peer column is "Source" (ingress) or
// "Target" (egress).
func writeAuditTable(b *strings.Builder, deps []model.NetworkDependency, peerLabel string) {
	fmt.Fprintf(b, "| %s | Port/Proto | Confidence | Evidence |\n", peerLabel)
	fmt.Fprintf(b, "|---|---|:-:|---|\n")
	for _, d := range deps {
		peer := d.Source
		if peerLabel == "Target" {
			peer = d.Target
		}
		conf := strings.ToUpper(string(d.Confidence))
		switch d.Confidence {
		case model.Low:
			conf = conf + " (investigate)"
		case model.Medium:
			conf = conf + " (review)"
		case model.High:
			conf = conf + " (approve)"
		}
		evidence := formatAuditEvidence(d)
		fmt.Fprintf(b, "| `%s` | `%d/%s` | %s | %s |\n",
			peer, d.Port, d.Protocol, conf, evidence)
	}
	fmt.Fprintf(b, "\n")
}

// formatAuditEvidence renders the file:line anchor and the redacted config
// line into one Markdown cell. Pipe characters in evidence are escaped so they
// do not break table rendering.
func formatAuditEvidence(d model.NetworkDependency) string {
	if d.SourceFile == "" {
		return "_no direct config line_"
	}
	if d.EvidenceLine == "" {
		return fmt.Sprintf("`%s`", d.SourceFile)
	}
	line := model.RedactSecrets(d.EvidenceLine)
	line = strings.ReplaceAll(line, "|", `\|`)
	// Trim very long evidence so the table stays readable.
	const maxLen = 80
	if len(line) > maxLen {
		line = line[:maxLen-3] + "..."
	}
	return fmt.Sprintf("`%s` &mdash; `%s`", d.SourceFile, line)
}

// workloadStatus returns a one-line state for the workload section, used to
// highlight workloads that may need reviewer attention before sign-off.
func workloadStatus(ingress, egress []model.NetworkDependency) string {
	if len(ingress) == 0 && len(egress) == 0 {
		return "no traffic declared"
	}
	hasLow := false
	hasMed := false
	for _, d := range ingress {
		if d.Confidence == model.Low {
			hasLow = true
		}
		if d.Confidence == model.Medium {
			hasMed = true
		}
	}
	for _, d := range egress {
		if d.Confidence == model.Low {
			hasLow = true
		}
		if d.Confidence == model.Medium {
			hasMed = true
		}
	}
	switch {
	case hasLow:
		return "REVIEW REQUIRED (low-confidence rows present)"
	case hasMed:
		return "review recommended (medium-confidence rows present)"
	default:
		return "ready for sign-off"
	}
}

// uniqueWorkloads returns the deterministic union of every Source and Target
// across the dependency set.
func uniqueWorkloads(deps []model.NetworkDependency) []string {
	seen := make(map[string]bool, len(deps)*2)
	for _, d := range deps {
		if d.Source != "" {
			seen[d.Source] = true
		}
		if d.Target != "" {
			seen[d.Target] = true
		}
	}
	out := make([]string, 0, len(seen))
	for w := range seen {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// tallyConfidence returns the high/medium/low counts for the slice.
func tallyConfidence(deps []model.NetworkDependency) (high, med, low int) {
	for _, d := range deps {
		switch d.Confidence {
		case model.High:
			high++
		case model.Medium:
			med++
		case model.Low:
			low++
		}
	}
	return
}

// countMissingEvidence returns how many deps have no inline EvidenceLine.
func countMissingEvidence(deps []model.NetworkDependency) int {
	n := 0
	for _, d := range deps {
		if d.EvidenceLine == "" {
			n++
		}
	}
	return n
}

// auditFingerprint is a short stable hash over the dependency keys of the
// set. It lets reviewers compare two audit ledger documents at a glance
// without diffing the full body. It excludes timestamps so identical inputs
// produce identical fingerprints.
func auditFingerprint(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	h := sha256.New()
	fmt.Fprintf(h, "service=%s\n", ds.ServiceName)
	for _, d := range deps {
		fmt.Fprintf(h, "%s|%s|%s\n", d.Key(), d.Confidence, d.SourceFile)
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:6])
}
