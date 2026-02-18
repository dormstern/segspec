# v0.4.0 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship per-service ingress policies, Helm template resolution, and interactive TUI to make segspec production-complete for platform engineers.

**Architecture:** Three features built sequentially. Feature 1 (ingress) adds `Sources()` to DependencySet and rewrites the renderer to emit per-service policies. Feature 2 (Helm) adds Chart.yaml detection to the walker and shells out to `helm template`. Feature 3 (interactive TUI) adds a bubbletea-based dependency picker activated by `--interactive`.

**Tech Stack:** Go 1.25, cobra (CLI), bubbletea + lipgloss (TUI), helm CLI (shelled out, not imported)

---

## Feature 1: Per-Service Ingress Policies

### Task 1.1: Add Sources() method to DependencySet

**Files:**
- Modify: `internal/model/dependency.go`
- Test: `internal/model/dependency_test.go`

**Step 1: Write the failing test**

In `internal/model/dependency_test.go`, add:

```go
func TestDependencySet_Sources(t *testing.T) {
	ds := NewDependencySet("myapp")
	ds.Add(NetworkDependency{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "frontend", Target: "productcatalog", Port: 3550, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "checkout", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP"})

	sources := ds.Sources()
	expected := []string{"cartservice", "checkout", "frontend"}
	if len(sources) != len(expected) {
		t.Fatalf("got %d sources, want %d: %v", len(sources), len(expected), sources)
	}
	for i, s := range sources {
		if s != expected[i] {
			t.Errorf("sources[%d] = %q, want %q", i, s, expected[i])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestDependencySet_Sources -v`
Expected: FAIL — `ds.Sources undefined`

**Step 3: Write minimal implementation**

In `internal/model/dependency.go`, add:

```go
// Sources returns a sorted, deduplicated list of all source service names.
func (ds *DependencySet) Sources() []string {
	seen := make(map[string]bool)
	for _, dep := range ds.deps {
		if dep.Source != "" {
			seen[dep.Source] = true
		}
	}
	sources := make([]string, 0, len(seen))
	for s := range seen {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	return sources
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -run TestDependencySet_Sources -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/dependency.go internal/model/dependency_test.go
git commit -m "feat: add Sources() to DependencySet for ingress graph inversion"
```

---

### Task 1.2: Add IngressFor() method to DependencySet

**Files:**
- Modify: `internal/model/dependency.go`
- Test: `internal/model/dependency_test.go`

**Step 1: Write the failing test**

```go
func TestDependencySet_IngressFor(t *testing.T) {
	ds := NewDependencySet("myapp")
	ds.Add(NetworkDependency{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "checkout", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "frontend", Target: "productcatalog", Port: 3550, Protocol: "TCP"})

	ingress := ds.IngressFor("cartservice")
	if len(ingress) != 2 {
		t.Fatalf("got %d ingress deps for cartservice, want 2", len(ingress))
	}
	// Should be sorted by Key()
	if ingress[0].Source != "checkout" {
		t.Errorf("ingress[0].Source = %q, want checkout", ingress[0].Source)
	}
	if ingress[1].Source != "frontend" {
		t.Errorf("ingress[1].Source = %q, want frontend", ingress[1].Source)
	}

	// Service with no ingress
	ingress = ds.IngressFor("redis")
	// redis has ingress from cartservice
	if len(ingress) != 1 {
		t.Fatalf("got %d ingress deps for redis, want 1", len(ingress))
	}

	// Unknown service
	ingress = ds.IngressFor("nonexistent")
	if len(ingress) != 0 {
		t.Fatalf("got %d ingress deps for nonexistent, want 0", len(ingress))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestDependencySet_IngressFor -v`
Expected: FAIL — `ds.IngressFor undefined`

**Step 3: Write minimal implementation**

```go
// IngressFor returns all dependencies whose Target matches the given service name,
// sorted by Key(). These represent inbound connections to that service.
func (ds *DependencySet) IngressFor(service string) []NetworkDependency {
	var result []NetworkDependency
	for _, dep := range ds.deps {
		if dep.Target == service {
			result = append(result, dep)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -run TestDependencySet_IngressFor -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/dependency.go internal/model/dependency_test.go
git commit -m "feat: add IngressFor() to DependencySet for ingress policy generation"
```

---

### Task 1.3: Add EgressFor() method to DependencySet

**Files:**
- Modify: `internal/model/dependency.go`
- Test: `internal/model/dependency_test.go`

**Step 1: Write the failing test**

```go
func TestDependencySet_EgressFor(t *testing.T) {
	ds := NewDependencySet("myapp")
	ds.Add(NetworkDependency{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "frontend", Target: "productcatalog", Port: 3550, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP"})

	egress := ds.EgressFor("frontend")
	if len(egress) != 2 {
		t.Fatalf("got %d egress deps for frontend, want 2", len(egress))
	}

	egress = ds.EgressFor("redis")
	if len(egress) != 0 {
		t.Fatalf("got %d egress deps for redis, want 0", len(egress))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestDependencySet_EgressFor -v`
Expected: FAIL — `ds.EgressFor undefined`

**Step 3: Write minimal implementation**

```go
// EgressFor returns all dependencies whose Source matches the given service name,
// sorted by Key(). These represent outbound connections from that service.
func (ds *DependencySet) EgressFor(service string) []NetworkDependency {
	var result []NetworkDependency
	for _, dep := range ds.deps {
		if dep.Source == service {
			result = append(result, dep)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -run TestDependencySet_EgressFor -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/dependency.go internal/model/dependency_test.go
git commit -m "feat: add EgressFor() to DependencySet for per-service egress filtering"
```

---

### Task 1.4: Rewrite renderer for per-service policies

**Files:**
- Modify: `internal/renderer/netpol.go`
- Test: `internal/renderer/netpol_test.go`

**Step 1: Write the failing test**

```go
func TestPerServiceNetworkPolicy(t *testing.T) {
	ds := model.NewDependencySet("myapp")
	ds.Add(model.NetworkDependency{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP"})
	ds.Add(model.NetworkDependency{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP"})

	output := PerServiceNetworkPolicy(ds)

	// Should contain policies for both frontend and cartservice
	if !strings.Contains(output, "name: frontend-netpol") {
		t.Error("missing frontend policy")
	}
	if !strings.Contains(output, "name: cartservice-netpol") {
		t.Error("missing cartservice policy")
	}

	// cartservice should have ingress from frontend
	if !strings.Contains(output, "app: frontend") {
		t.Error("missing ingress from frontend in cartservice policy")
	}

	// cartservice should have egress to redis
	if !strings.Contains(output, "app: redis") {
		t.Error("missing egress to redis in cartservice policy")
	}

	// frontend should have egress to cartservice but no ingress section
	// (no one talks to frontend in this dataset)

	// Both should have Ingress and Egress in policyTypes
	if strings.Count(output, "- Ingress") < 2 {
		t.Error("expected Ingress policyType in each service policy")
	}
	if strings.Count(output, "- Egress") < 2 {
		t.Error("expected Egress policyType in each service policy")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/renderer/ -run TestPerServiceNetworkPolicy -v`
Expected: FAIL — `PerServiceNetworkPolicy undefined`

**Step 3: Write minimal implementation**

Add `PerServiceNetworkPolicy()` to `internal/renderer/netpol.go`:

```go
// PerServiceNetworkPolicy generates one NetworkPolicy per service with both
// ingress and egress rules. Each policy includes:
// - Default-deny for both directions
// - Egress rules for what this service talks to
// - Ingress rules for what talks to this service
// - DNS egress to kube-system
func PerServiceNetworkPolicy(ds *model.DependencySet) string {
	sources := ds.Sources()
	if len(sources) == 0 {
		return ""
	}

	// Also collect targets that are not sources (leaf services like redis)
	allServices := make(map[string]bool)
	for _, s := range sources {
		allServices[s] = true
	}
	for _, dep := range ds.Dependencies() {
		if dep.Target != "" {
			allServices[dep.Target] = true
		}
	}
	serviceList := make([]string, 0, len(allServices))
	for s := range allServices {
		serviceList = append(serviceList, s)
	}
	sort.Strings(serviceList)

	var b strings.Builder
	for i, svc := range serviceList {
		if i > 0 {
			fmt.Fprintf(&b, "---\n")
		}
		svcName := sanitizeName(svc)
		egress := ds.EgressFor(svc)
		ingress := ds.IngressFor(svc)

		fmt.Fprintf(&b, "apiVersion: networking.k8s.io/v1\n")
		fmt.Fprintf(&b, "kind: NetworkPolicy\n")
		fmt.Fprintf(&b, "metadata:\n")
		fmt.Fprintf(&b, "  name: %s-netpol\n", svcName)
		fmt.Fprintf(&b, "  labels:\n")
		fmt.Fprintf(&b, "    generated-by: segspec\n")
		fmt.Fprintf(&b, "spec:\n")
		fmt.Fprintf(&b, "  podSelector:\n")
		fmt.Fprintf(&b, "    matchLabels:\n")
		fmt.Fprintf(&b, "      app: %s\n", svcName)
		fmt.Fprintf(&b, "  policyTypes:\n")
		fmt.Fprintf(&b, "    - Ingress\n")
		fmt.Fprintf(&b, "    - Egress\n")

		// Ingress rules
		if len(ingress) > 0 {
			fmt.Fprintf(&b, "  ingress:\n")
			for _, dep := range ingress {
				if dep.Source == "" {
					continue
				}
				renderIngressFrom(&b, dep.Source)
				if dep.Port > 0 {
					proto := strings.ToUpper(dep.Protocol)
					if proto == "" {
						proto = "TCP"
					}
					fmt.Fprintf(&b, "      ports:\n")
					fmt.Fprintf(&b, "        - port: %d\n", dep.Port)
					fmt.Fprintf(&b, "          protocol: %s\n", proto)
				}
			}
		}

		// Egress rules
		if len(egress) > 0 {
			fmt.Fprintf(&b, "  egress:\n")
			for _, dep := range egress {
				if dep.Port <= 0 {
					continue
				}
				proto := strings.ToUpper(dep.Protocol)
				if proto == "" {
					proto = "TCP"
				}
				renderEgressTo(&b, dep.Target)
				fmt.Fprintf(&b, "      ports:\n")
				fmt.Fprintf(&b, "        - port: %d\n", dep.Port)
				fmt.Fprintf(&b, "          protocol: %s\n", proto)
			}
			// DNS egress
			fmt.Fprintf(&b, "    - to:\n")
			fmt.Fprintf(&b, "        - namespaceSelector:\n")
			fmt.Fprintf(&b, "            matchLabels:\n")
			fmt.Fprintf(&b, "              kubernetes.io/metadata.name: kube-system\n")
			fmt.Fprintf(&b, "      ports:\n")
			fmt.Fprintf(&b, "        - port: 53\n")
			fmt.Fprintf(&b, "          protocol: UDP\n")
			fmt.Fprintf(&b, "        - port: 53\n")
			fmt.Fprintf(&b, "          protocol: TCP\n")
		}
	}

	return b.String()
}

// renderIngressFrom writes the `from:` block for an ingress rule.
func renderIngressFrom(b *strings.Builder, source string) {
	if ip := net.ParseIP(source); ip != nil {
		fmt.Fprintf(b, "    - from:\n")
		fmt.Fprintf(b, "        - ipBlock:\n")
		fmt.Fprintf(b, "            cidr: %s/32\n", source)
	} else if strings.Contains(source, ".") {
		parts := strings.SplitN(source, ".", 3)
		svcName := parts[0]
		namespace := ""
		if len(parts) >= 2 {
			namespace = parts[1]
		}
		fmt.Fprintf(b, "    - from:\n")
		fmt.Fprintf(b, "        - podSelector:\n")
		fmt.Fprintf(b, "            matchLabels:\n")
		fmt.Fprintf(b, "              app: %s\n", svcName)
		if namespace != "" {
			fmt.Fprintf(b, "          namespaceSelector:\n")
			fmt.Fprintf(b, "            matchLabels:\n")
			fmt.Fprintf(b, "              kubernetes.io/metadata.name: %s\n", namespace)
		}
	} else {
		fmt.Fprintf(b, "    - from:\n")
		fmt.Fprintf(b, "        - podSelector:\n")
		fmt.Fprintf(b, "            matchLabels:\n")
		fmt.Fprintf(b, "              app: %s\n", source)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/renderer/ -run TestPerServiceNetworkPolicy -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/renderer/netpol.go internal/renderer/netpol_test.go
git commit -m "feat: per-service NetworkPolicy generation with ingress + egress"
```

---

### Task 1.5: Wire per-service format into CLI

**Files:**
- Modify: `cmd/analyze.go`
- Test: `cmd/analyze_test.go`

**Step 1: Add `per-service` format option**

In `cmd/analyze.go`, update the `switch outputFormat` block:

```go
case "per-service":
	fmt.Fprint(out, renderer.PerServiceNetworkPolicy(ds))
```

Update the `--format` flag default description and error message to include `per-service`.

**Step 2: Update the Long description** in `analyzeCmd` to mention `--format per-service`.

**Step 3: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add cmd/analyze.go
git commit -m "feat: add --format per-service for per-service ingress+egress policies"
```

---

## Feature 2: Helm Template Resolution

### Task 2.1: Add Helm chart detection to walker

**Files:**
- Modify: `internal/walker/walker.go`
- Test: `internal/walker/walker_test.go`
- Create: test fixture `internal/walker/testdata/helm-app/Chart.yaml`
- Create: test fixture `internal/walker/testdata/helm-app/templates/deployment.yaml`
- Create: test fixture `internal/walker/testdata/helm-app/values.yaml`

**Step 1: Create test fixtures**

`internal/walker/testdata/helm-app/Chart.yaml`:
```yaml
apiVersion: v2
name: helm-app
version: 0.1.0
```

`internal/walker/testdata/helm-app/values.yaml`:
```yaml
image:
  repository: myapp
  tag: latest
redis:
  host: redis-cache
  port: 6379
```

`internal/walker/testdata/helm-app/templates/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Values.image.repository }}
spec:
  template:
    spec:
      containers:
        - name: app
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          ports:
            - containerPort: 8080
```

**Step 2: Write the failing test**

In `internal/walker/walker_test.go`:

```go
func TestDetectHelmCharts(t *testing.T) {
	charts := detectHelmCharts("testdata/helm-app")
	if len(charts) != 1 {
		t.Fatalf("got %d charts, want 1", len(charts))
	}
	if charts[0] != "testdata/helm-app" {
		t.Errorf("chart path = %q, want %q", charts[0], "testdata/helm-app")
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/walker/ -run TestDetectHelmCharts -v`
Expected: FAIL — `detectHelmCharts undefined`

**Step 4: Implement detectHelmCharts**

In `internal/walker/walker.go`:

```go
// detectHelmCharts finds directories containing Chart.yaml under root.
func detectHelmCharts(root string) []string {
	var charts []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Chart.yaml" {
			charts = append(charts, filepath.Dir(path))
		}
		return nil
	})
	return charts
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/walker/ -run TestDetectHelmCharts -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/walker/walker.go internal/walker/walker_test.go internal/walker/testdata/
git commit -m "feat: detect Helm charts by finding Chart.yaml"
```

---

### Task 2.2: Shell out to helm template

**Files:**
- Modify: `internal/walker/walker.go`
- Create: `internal/walker/helm.go`
- Test: `internal/walker/helm_test.go`

**Step 1: Write the failing test**

In `internal/walker/helm_test.go`:

```go
func TestRenderHelmTemplate(t *testing.T) {
	// Test with no helm binary — should return error gracefully
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/walker/ -run TestRenderHelmTemplate -v`
Expected: FAIL — `renderHelmTemplate undefined`

**Step 3: Implement renderHelmTemplate**

In `internal/walker/helm.go`:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/walker/ -run TestRenderHelmTemplate -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/walker/helm.go internal/walker/helm_test.go
git commit -m "feat: shell out to helm template with 30s timeout"
```

---

### Task 2.3: Parse rendered Helm output and integrate into walker

**Files:**
- Modify: `internal/walker/walker.go`
- Modify: `internal/walker/helm.go`
- Modify: `internal/parser/k8s.go` (need to expose a ParseK8sYAML function that accepts string content)
- Test: `internal/walker/walker_test.go`

**Step 1: Add ParseK8sContent to k8s parser**

The existing K8s parser reads from a file. We need a version that parses YAML content directly (for helm template output). In `internal/parser/k8s.go`, add:

```go
// ParseK8sContent parses K8s manifest YAML content (multi-document) and returns
// discovered network dependencies. Used for parsing helm template output.
func ParseK8sContent(content string, sourceLabel string) ([]model.NetworkDependency, error) {
	// Same logic as ParseK8sManifest but reads from string instead of file
	// ... (reuse the existing parsing logic with a bytes reader)
}
```

**Step 2: Integrate Helm charts into Walk**

In `internal/walker/walker.go`, after the existing `filepath.WalkDir` loop, add Helm chart handling:

```go
// After normal file walk, detect and process Helm charts
charts := detectHelmCharts(root)
for _, chartDir := range charts {
	rendered, err := renderHelmTemplate(chartDir, "")
	if err != nil {
		relPath, _ := filepath.Rel(root, chartDir)
		warnings = append(warnings, WalkWarning{File: relPath + "/Chart.yaml", Err: err})
		continue
	}
	relPath, _ := filepath.Rel(root, chartDir)
	deps, parseErr := parser.ParseK8sContent(rendered, relPath+"/Chart.yaml (helm template)")
	if parseErr != nil {
		warnings = append(warnings, WalkWarning{File: relPath, Err: parseErr})
		continue
	}
	for i := range deps {
		if deps[i].Source == "" {
			deps[i].Source = serviceName
		}
		ds.Add(deps[i])
	}
}
```

**Step 3: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add internal/walker/walker.go internal/walker/helm.go internal/parser/k8s.go internal/parser/k8s_test.go
git commit -m "feat: parse Helm chart rendered output in walker"
```

---

### Task 2.4: Add --helm-values flag to CLI

**Files:**
- Modify: `cmd/analyze.go`

**Step 1: Add the flag**

```go
var helmValuesFile string

func init() {
	analyzeCmd.Flags().StringVar(&helmValuesFile, "helm-values", "", "Helm values file to use when rendering charts")
}
```

**Step 2: Pass helmValuesFile through to walker**

This requires modifying `walker.Walk` to accept an optional `WalkOptions` struct:

```go
type WalkOptions struct {
	HelmValuesFile string
}
```

Update `Walk` signature: `Walk(root string, registry *parser.Registry, opts WalkOptions)`

**Step 3: Update cmd/analyze.go** to pass the option.

**Step 4: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add cmd/analyze.go internal/walker/walker.go internal/walker/walker_test.go
git commit -m "feat: add --helm-values flag for custom Helm values file"
```

---

## Feature 3: Interactive TUI

### Task 3.1: Add bubbletea dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add dependencies**

```bash
cd /Users/dormorgenstern/proj_dor/segspec
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

**Step 2: Verify**

Run: `go mod tidy`
Expected: go.mod and go.sum updated, no errors

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add bubbletea + lipgloss dependencies for interactive TUI"
```

---

### Task 3.2: Build the TUI model

**Files:**
- Create: `internal/tui/picker.go`
- Test: `internal/tui/picker_test.go`

**Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestNewPicker(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
		{Source: "frontend", Target: "adservice", Port: 9555, Protocol: "TCP", Confidence: model.Medium},
	}

	p := NewPicker(deps)
	if len(p.items) != 2 {
		t.Fatalf("got %d items, want 2", len(p.items))
	}
	// All should start selected
	for i, item := range p.items {
		if !item.selected {
			t.Errorf("item[%d] should be selected by default", i)
		}
	}
}

func TestPicker_Toggle(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
	}
	p := NewPicker(deps)
	p.toggle(0)
	if p.items[0].selected {
		t.Error("item should be deselected after toggle")
	}
	p.toggle(0)
	if !p.items[0].selected {
		t.Error("item should be selected after second toggle")
	}
}

func TestPicker_Selected(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
		{Source: "frontend", Target: "adservice", Port: 9555, Protocol: "TCP", Confidence: model.Medium},
	}
	p := NewPicker(deps)
	p.toggle(1) // deselect adservice

	selected := p.Selected()
	if len(selected) != 1 {
		t.Fatalf("got %d selected, want 1", len(selected))
	}
	if selected[0].Target != "cartservice" {
		t.Errorf("selected[0].Target = %q, want cartservice", selected[0].Target)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -v`
Expected: FAIL — package doesn't exist

**Step 3: Implement the picker model**

In `internal/tui/picker.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dormstern/segspec/internal/model"
)

type item struct {
	dep      model.NetworkDependency
	selected bool
}

// Picker is a bubbletea model for selecting dependencies.
type Picker struct {
	items    []item
	cursor   int
	quitted  bool
	confirmed bool
}

// NewPicker creates a picker with all deps selected by default.
func NewPicker(deps []model.NetworkDependency) *Picker {
	items := make([]item, len(deps))
	for i, d := range deps {
		items[i] = item{dep: d, selected: true}
	}
	return &Picker{items: items}
}

func (p *Picker) toggle(i int) {
	if i >= 0 && i < len(p.items) {
		p.items[i].selected = !p.items[i].selected
	}
}

func (p *Picker) selectAll() {
	for i := range p.items {
		p.items[i].selected = true
	}
}

func (p *Picker) selectNone() {
	for i := range p.items {
		p.items[i].selected = false
	}
}

// Selected returns the dependencies the user accepted.
func (p *Picker) Selected() []model.NetworkDependency {
	var result []model.NetworkDependency
	for _, it := range p.items {
		if it.selected {
			result = append(result, it.dep)
		}
	}
	return result
}

// Confirmed returns true if user pressed Enter (not q).
func (p *Picker) Confirmed() bool {
	return p.confirmed
}

func (p *Picker) Init() tea.Cmd {
	return nil
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			p.quitted = true
			return p, tea.Quit
		case "enter":
			p.confirmed = true
			return p, tea.Quit
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case " ":
			p.toggle(p.cursor)
		case "a":
			p.selectAll()
		case "n":
			p.selectNone()
		}
	}
	return p, nil
}

var (
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	unselectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	cursorStyle     = lipgloss.NewStyle().Bold(true)
	headerStyle     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	helpStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (p *Picker) View() string {
	var b strings.Builder

	selected := 0
	for _, it := range p.items {
		if it.selected {
			selected++
		}
	}

	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render(
		fmt.Sprintf("segspec: %d dependencies found (%d selected)", len(p.items), selected)))

	for i, it := range p.items {
		cursor := "  "
		if i == p.cursor {
			cursor = "> "
		}

		checkbox := "[ ]"
		style := unselectedStyle
		if it.selected {
			checkbox = "[x]"
			style = selectedStyle
		}

		line := fmt.Sprintf("%s %s → %s:%d/%s  [%s]  %s",
			checkbox,
			it.dep.Source, it.dep.Target, it.dep.Port, it.dep.Protocol,
			it.dep.Confidence, it.dep.Description)

		if i == p.cursor {
			fmt.Fprintf(&b, "%s%s\n", cursorStyle.Render(cursor), style.Render(line))
		} else {
			fmt.Fprintf(&b, "%s%s\n", cursor, style.Render(line))
		}
	}

	fmt.Fprintf(&b, "\n%s\n", helpStyle.Render(
		"↑/↓ navigate  SPACE toggle  a all  n none  ENTER generate  q quit"))

	return b.String()
}

// Run launches the interactive picker and returns the selected dependencies.
// Returns nil, false if user quit without confirming.
func Run(deps []model.NetworkDependency) ([]model.NetworkDependency, bool) {
	p := NewPicker(deps)
	prog := tea.NewProgram(p)
	finalModel, err := prog.Run()
	if err != nil {
		return nil, false
	}
	picker := finalModel.(*Picker)
	if !picker.Confirmed() {
		return nil, false
	}
	return picker.Selected(), true
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tui/picker.go internal/tui/picker_test.go
git commit -m "feat: interactive TUI dependency picker with bubbletea"
```

---

### Task 3.3: Wire --interactive flag into CLI

**Files:**
- Modify: `cmd/analyze.go`

**Step 1: Add the flag and TTY check**

```go
var interactive bool

func init() {
	analyzeCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Review dependencies interactively before generating output")
}
```

**Step 2: Add TUI invocation** in `runAnalyze`, after AI analysis and before rendering:

```go
if interactive {
	// Check if stdout is a TTY
	if fileInfo, _ := os.Stdout.Stat(); fileInfo.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintln(os.Stderr, "Warning: --interactive requires a terminal, falling back to non-interactive")
	} else {
		selected, ok := tui.Run(ds.Dependencies())
		if !ok {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
		// Rebuild DependencySet with only selected deps
		filtered := model.NewDependencySet(ds.ServiceName)
		for _, dep := range selected {
			filtered.Add(dep)
		}
		ds = filtered
	}
}
```

**Step 3: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 4: Commit**

```bash
git add cmd/analyze.go
git commit -m "feat: add --interactive flag for TUI dependency review"
```

---

### Task 3.4: Final integration test and cleanup

**Files:**
- All modified files

**Step 1: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all 8 packages PASS (new `tui` package added)

**Step 2: Run go vet and verify no issues**

Run: `go vet ./...`
Expected: no output (clean)

**Step 3: Update README.md**

- Add `--format per-service` to output formats section
- Add Helm support to the supported config types table
- Add `--interactive` / `-i` to usage examples
- Update roadmap (remove completed items, keep GitHub Action + multi-format)

**Step 4: Commit and tag**

```bash
git add .
git commit -m "feat: v0.4.0 — per-service ingress, Helm support, interactive TUI"
git tag -a v0.4.0 -m "v0.4.0: Complete Microseg — per-service ingress policies, Helm template resolution, interactive TUI"
git push origin main --tags
```
