package renderer

import (
	"fmt"
	"strings"

	"github.com/dormorgenstern/segspec/internal/model"
)

// NetworkPolicy renders Kubernetes NetworkPolicy YAML from dependencies.
func NetworkPolicy(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return ""
	}

	var b strings.Builder

	// Group egress rules by target:port
	type egressRule struct {
		target   string
		port     int
		protocol string
	}
	rules := make([]egressRule, 0, len(deps))
	seen := make(map[string]bool)
	for _, dep := range deps {
		key := fmt.Sprintf("%s:%d:%s", dep.Target, dep.Port, dep.Protocol)
		if !seen[key] {
			seen[key] = true
			rules = append(rules, egressRule{dep.Target, dep.Port, dep.Protocol})
		}
	}

	// Render NetworkPolicy
	fmt.Fprintf(&b, "apiVersion: networking.k8s.io/v1\n")
	fmt.Fprintf(&b, "kind: NetworkPolicy\n")
	fmt.Fprintf(&b, "metadata:\n")
	fmt.Fprintf(&b, "  name: %s-egress\n", sanitizeName(ds.ServiceName))
	fmt.Fprintf(&b, "  labels:\n")
	fmt.Fprintf(&b, "    generated-by: segspec\n")
	fmt.Fprintf(&b, "spec:\n")
	fmt.Fprintf(&b, "  podSelector:\n")
	fmt.Fprintf(&b, "    matchLabels:\n")
	fmt.Fprintf(&b, "      app: %s\n", sanitizeName(ds.ServiceName))
	fmt.Fprintf(&b, "  policyTypes:\n")
	fmt.Fprintf(&b, "    - Egress\n")
	fmt.Fprintf(&b, "  egress:\n")

	for _, rule := range rules {
		proto := strings.ToUpper(rule.protocol)
		if proto == "" {
			proto = "TCP"
		}
		fmt.Fprintf(&b, "    - to: # %s\n", rule.target)
		fmt.Fprintf(&b, "      ports:\n")
		fmt.Fprintf(&b, "        - port: %d\n", rule.port)
		fmt.Fprintf(&b, "          protocol: %s\n", proto)
	}

	// Also render DNS egress (port 53) — almost always needed
	fmt.Fprintf(&b, "    - to: # DNS\n")
	fmt.Fprintf(&b, "      ports:\n")
	fmt.Fprintf(&b, "        - port: 53\n")
	fmt.Fprintf(&b, "          protocol: UDP\n")
	fmt.Fprintf(&b, "        - port: 53\n")
	fmt.Fprintf(&b, "          protocol: TCP\n")

	return b.String()
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
	return s
}
