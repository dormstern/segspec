package renderer

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

// NetworkPolicy renders Kubernetes NetworkPolicy YAML from dependencies.
// It generates two policies:
// 1. A default-deny policy (blocks all ingress and egress)
// 2. An allow policy for known egress destinations with proper selectors
func NetworkPolicy(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return ""
	}

	// Group egress rules by target:port, skipping invalid ports (Fix 2)
	type egressRule struct {
		target   string
		port     int
		protocol string
	}
	rules := make([]egressRule, 0, len(deps))
	seen := make(map[string]bool)
	var skipped []string
	for _, dep := range deps {
		if dep.Port <= 0 {
			skipped = append(skipped, dep.Target)
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", dep.Target, dep.Port, dep.Protocol)
		if !seen[key] {
			seen[key] = true
			rules = append(rules, egressRule{dep.Target, dep.Port, dep.Protocol})
		}
	}

	// Deduplicate and sort skipped targets for deterministic output
	skipped = dedupeStrings(skipped)

	svcName := sanitizeName(ds.ServiceName)

	var b strings.Builder

	// --- Policy 1: Default deny (both ingress and egress) --- (Fix 4)
	// Review: verify podSelector labels match your deployment
	fmt.Fprintf(&b, "# Review: verify podSelector labels match your deployment\n")
	fmt.Fprintf(&b, "apiVersion: networking.k8s.io/v1\n")
	fmt.Fprintf(&b, "kind: NetworkPolicy\n")
	fmt.Fprintf(&b, "metadata:\n")
	fmt.Fprintf(&b, "  name: %s-default-deny\n", svcName)
	fmt.Fprintf(&b, "  labels:\n")
	fmt.Fprintf(&b, "    generated-by: segspec\n")
	fmt.Fprintf(&b, "spec:\n")
	fmt.Fprintf(&b, "  podSelector:\n")
	fmt.Fprintf(&b, "    matchLabels:\n")
	fmt.Fprintf(&b, "      app: %s\n", svcName)
	fmt.Fprintf(&b, "  policyTypes:\n")
	fmt.Fprintf(&b, "    - Ingress\n")
	fmt.Fprintf(&b, "    - Egress\n")
	fmt.Fprintf(&b, "# Default deny policy — blocks all traffic not explicitly allowed above\n")

	// --- Policy 2: Allow egress rules ---
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "apiVersion: networking.k8s.io/v1\n")
	fmt.Fprintf(&b, "kind: NetworkPolicy\n")
	fmt.Fprintf(&b, "metadata:\n")
	fmt.Fprintf(&b, "  name: %s-egress\n", svcName)
	fmt.Fprintf(&b, "  labels:\n")
	fmt.Fprintf(&b, "    generated-by: segspec\n")
	fmt.Fprintf(&b, "spec:\n")
	fmt.Fprintf(&b, "  podSelector:\n")
	fmt.Fprintf(&b, "    matchLabels:\n")
	fmt.Fprintf(&b, "      app: %s\n", svcName)
	fmt.Fprintf(&b, "  policyTypes:\n")
	fmt.Fprintf(&b, "    - Egress\n")
	fmt.Fprintf(&b, "  egress:\n")

	// Render each egress rule with proper destination selectors (Fix 1)
	for _, rule := range rules {
		proto := strings.ToUpper(rule.protocol)
		if proto == "" {
			proto = "TCP"
		}
		renderEgressTo(&b, rule.target)
		fmt.Fprintf(&b, "      ports:\n")
		fmt.Fprintf(&b, "        - port: %d\n", rule.port)
		fmt.Fprintf(&b, "          protocol: %s\n", proto)
	}

	// DNS egress (port 53) restricted to kube-system namespace (Fix 1)
	fmt.Fprintf(&b, "    - to:\n")
	fmt.Fprintf(&b, "        - namespaceSelector:\n")
	fmt.Fprintf(&b, "            matchLabels:\n")
	fmt.Fprintf(&b, "              kubernetes.io/metadata.name: kube-system\n")
	fmt.Fprintf(&b, "      ports:\n")
	fmt.Fprintf(&b, "        - port: 53\n")
	fmt.Fprintf(&b, "          protocol: UDP\n")
	fmt.Fprintf(&b, "        - port: 53\n")
	fmt.Fprintf(&b, "          protocol: TCP\n")

	// Append comment listing skipped dependencies (Fix 2)
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "# Skipped (no port): %s\n", strings.Join(skipped, ", "))
	}

	return b.String()
}

// PerServiceNetworkPolicy generates one NetworkPolicy per service with both
// ingress and egress rules. Each policy includes:
// - Default-deny for both directions (via policyTypes)
// - Ingress rules for what talks to this service
// - Egress rules for what this service talks to
// - DNS egress to kube-system for services that have egress
func PerServiceNetworkPolicy(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return ""
	}

	// Discover all services (both sources and targets)
	allServices := make(map[string]bool)
	for _, dep := range deps {
		if dep.Source != "" {
			allServices[dep.Source] = true
		}
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
// Mirrors renderEgressTo but uses `from:` instead of `to:`.
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

// renderEgressTo writes the appropriate `to:` block for the given target.
// - Simple service name (no dots, no IP) -> podSelector with app label
// - FQDN (contains dots, not an IP) -> podSelector + namespaceSelector
// - IP address -> ipBlock with /32 CIDR
func renderEgressTo(b *strings.Builder, target string) {
	if ip := net.ParseIP(target); ip != nil {
		// IP address target
		fmt.Fprintf(b, "    - to:\n")
		fmt.Fprintf(b, "        - ipBlock:\n")
		fmt.Fprintf(b, "            cidr: %s/32\n", target)
	} else if strings.Contains(target, ".") {
		// FQDN target — extract service name and namespace
		parts := strings.SplitN(target, ".", 3)
		svcName := parts[0]
		namespace := ""
		if len(parts) >= 2 {
			namespace = parts[1]
		}
		fmt.Fprintf(b, "    - to:\n")
		fmt.Fprintf(b, "        - podSelector:\n")
		fmt.Fprintf(b, "            matchLabels:\n")
		fmt.Fprintf(b, "              app: %s\n", svcName)
		if namespace != "" {
			fmt.Fprintf(b, "          namespaceSelector:\n")
			fmt.Fprintf(b, "            matchLabels:\n")
			fmt.Fprintf(b, "              kubernetes.io/metadata.name: %s\n", namespace)
		}
	} else {
		// Simple service name
		fmt.Fprintf(b, "    - to:\n")
		fmt.Fprintf(b, "        - podSelector:\n")
		fmt.Fprintf(b, "            matchLabels:\n")
		fmt.Fprintf(b, "              app: %s\n", target)
	}
}

// sanitizeName converts a string to a valid K8s resource name.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = s[:63]
	}
	// Fix 3: re-trim trailing hyphens after truncation
	s = strings.TrimRight(s, "-")
	// Fix 3: default to "unknown" if empty after sanitization
	if s == "" {
		s = "unknown"
	}
	return s
}

// dedupeStrings returns a sorted, deduplicated copy of the input.
func dedupeStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
