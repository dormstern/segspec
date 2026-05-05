package renderer

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

// Cilium renders a CiliumNetworkPolicy CRD (cilium.io/v2) for the analyzed
// service. The CNP shape differs from vanilla NetworkPolicy in three ways
// the validator (and this renderer) care about:
//
//  1. The workload anchor is `endpointSelector` (not `podSelector`).
//  2. External hostnames go in `egress[].toFQDNs[]` — `matchName` for
//     concrete DNS names, `matchPattern` for wildcards. In-cluster service
//     names route through `egress[].toEndpoints[]` instead, mirroring the
//     vanilla podSelector shape.
//  3. IP-shape destinations use `egress[].toCIDR[]`.
//
// Self-consistency contract: when ANY toFQDNs entry is emitted, this
// renderer also emits a paired DNS egress to kube-system port 53 with a
// dns matchPattern "*" — exactly what the validator's MissingDNSEgress
// check (cilium/cilium#44504) demands. Tests assert the rendered YAML
// passes the validator. Segspec must not emit policies that fail its
// own linter.
//
// Per-dep Disabled semantics inherit from the vanilla path: deps with
// Disabled="egress" or Disabled="full" are skipped entirely.
//
// Free-tier feature. Out of scope for v0.6.0: L7 rules, toServices,
// per-service-fan-out (one CNP per workload).
func Cilium(ds *model.DependencySet) string {
	deps := ds.Dependencies()
	if len(deps) == 0 {
		return ""
	}

	// Bucket each dep into the right Cilium destination shape, skipping
	// disabled-egress deps. Sorting inside each bucket keeps the output
	// deterministic regardless of input order.
	type endpointDest struct {
		name string // simple service name → app label
		port int
		prot string
	}
	type fqdnDest struct {
		host string
		port int
		prot string
	}
	type cidrDest struct {
		cidr string
		port int
		prot string
	}

	var (
		endpoints []endpointDest
		fqdns     []fqdnDest
		cidrs     []cidrDest
		skipped   []string
	)

	dedup := make(map[string]bool)
	for _, dep := range deps {
		if dep.Disabled == "egress" || dep.Disabled == "full" {
			continue
		}
		if dep.Port <= 0 {
			skipped = append(skipped, dep.Target)
			continue
		}
		key := fmt.Sprintf("%s|%d|%s", dep.Target, dep.Port, dep.Protocol)
		if dedup[key] {
			continue
		}
		dedup[key] = true

		prot := strings.ToUpper(dep.Protocol)
		if prot == "" {
			prot = "TCP"
		}

		switch ciliumShapeOf(dep.Target) {
		case shapeCIDR:
			cidrs = append(cidrs, cidrDest{cidr: dep.Target + "/32", port: dep.Port, prot: prot})
		case shapeFQDN:
			fqdns = append(fqdns, fqdnDest{host: dep.Target, port: dep.Port, prot: prot})
		default: // shapeEndpoint
			// In-cluster FQDNs like "postgres.production.svc.cluster.local"
			// keep just the leading service name as the app label, mirroring
			// renderEgressTo's heuristic.
			name := dep.Target
			if i := strings.Index(name, "."); i >= 0 {
				name = name[:i]
			}
			endpoints = append(endpoints, endpointDest{name: name, port: dep.Port, prot: prot})
		}
	}

	skipped = dedupeStrings(skipped)

	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].name != endpoints[j].name {
			return endpoints[i].name < endpoints[j].name
		}
		if endpoints[i].port != endpoints[j].port {
			return endpoints[i].port < endpoints[j].port
		}
		return endpoints[i].prot < endpoints[j].prot
	})
	sort.Slice(fqdns, func(i, j int) bool {
		if fqdns[i].host != fqdns[j].host {
			return fqdns[i].host < fqdns[j].host
		}
		if fqdns[i].port != fqdns[j].port {
			return fqdns[i].port < fqdns[j].port
		}
		return fqdns[i].prot < fqdns[j].prot
	})
	sort.Slice(cidrs, func(i, j int) bool {
		if cidrs[i].cidr != cidrs[j].cidr {
			return cidrs[i].cidr < cidrs[j].cidr
		}
		if cidrs[i].port != cidrs[j].port {
			return cidrs[i].port < cidrs[j].port
		}
		return cidrs[i].prot < cidrs[j].prot
	})

	svcName := sanitizeName(ds.ServiceName)

	var b strings.Builder
	b.WriteString("# Review: verify endpointSelector labels match your deployment\n")
	b.WriteString("apiVersion: cilium.io/v2\n")
	b.WriteString("kind: CiliumNetworkPolicy\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s-cnp\n", svcName)
	b.WriteString("  labels:\n")
	b.WriteString("    generated-by: segspec\n")
	b.WriteString("spec:\n")
	b.WriteString("  endpointSelector:\n")
	b.WriteString("    matchLabels:\n")
	fmt.Fprintf(&b, "      app: %s\n", svcName)

	hasEgress := len(endpoints) > 0 || len(fqdns) > 0 || len(cidrs) > 0
	if hasEgress {
		b.WriteString("  egress:\n")
		for _, e := range endpoints {
			b.WriteString("    - toEndpoints:\n")
			b.WriteString("        - matchLabels:\n")
			fmt.Fprintf(&b, "            app: %s\n", e.name)
			writeCiliumPort(&b, e.port, e.prot)
		}
		for _, f := range fqdns {
			b.WriteString("    - toFQDNs:\n")
			if strings.ContainsAny(f.host, "*?") {
				fmt.Fprintf(&b, "        - matchPattern: %q\n", f.host)
			} else {
				fmt.Fprintf(&b, "        - matchName: %q\n", f.host)
			}
			writeCiliumPort(&b, f.port, f.prot)
		}
		for _, c := range cidrs {
			b.WriteString("    - toCIDR:\n")
			fmt.Fprintf(&b, "        - %s\n", c.cidr)
			writeCiliumPort(&b, c.port, c.prot)
		}

		// Paired DNS egress: emitted once whenever any toFQDNs rule is
		// present, so the validator's MissingDNSEgress check (#44504) is
		// satisfied by construction. We use the kube-system kube-dns
		// shape that's idiomatic in Cilium docs.
		if len(fqdns) > 0 {
			b.WriteString("    - toEndpoints:\n")
			b.WriteString("        - matchLabels:\n")
			b.WriteString("            k8s:io.kubernetes.pod.namespace: kube-system\n")
			b.WriteString("            k8s:k8s-app: kube-dns\n")
			b.WriteString("      toPorts:\n")
			b.WriteString("        - ports:\n")
			b.WriteString("            - port: \"53\"\n")
			b.WriteString("              protocol: UDP\n")
			b.WriteString("            - port: \"53\"\n")
			b.WriteString("              protocol: TCP\n")
			b.WriteString("          rules:\n")
			b.WriteString("            dns:\n")
			b.WriteString("              - matchPattern: \"*\"\n")
		}
	}

	if len(skipped) > 0 {
		fmt.Fprintf(&b, "# Skipped (no port): %s\n", strings.Join(skipped, ", "))
	}

	return b.String()
}

// writeCiliumPort emits the toPorts block shared by every egress rule.
// Ports are stringified to keep YAML stable and tolerate non-numeric
// future port-name extensions; the validator parses both forms.
func writeCiliumPort(b *strings.Builder, port int, prot string) {
	b.WriteString("      toPorts:\n")
	b.WriteString("        - ports:\n")
	fmt.Fprintf(b, "            - port: %q\n", fmt.Sprintf("%d", port))
	fmt.Fprintf(b, "              protocol: %s\n", prot)
}

// shapeOf classifies a destination string into one of the three Cilium
// peer shapes.
type ciliumShape int

const (
	shapeEndpoint ciliumShape = iota // in-cluster service / simple name
	shapeFQDN                        // external hostname (api.stripe.com, *.amazonaws.com)
	shapeCIDR                        // literal IP
)

// ciliumShapeOf chooses the destination shape using the same heuristic
// the per-service vanilla renderer uses for namespace detection: a
// trailing ".svc" / ".cluster" / ".local" segment, or a bare segment
// with no dots, signals in-cluster. Anything else with dots and no IP
// shape is treated as an external FQDN.
func ciliumShapeOf(target string) ciliumShape {
	if target == "" {
		return shapeEndpoint
	}
	if ip := net.ParseIP(target); ip != nil {
		return shapeCIDR
	}
	if !strings.Contains(target, ".") {
		// Bare service name → endpoint.
		return shapeEndpoint
	}
	// Wildcards are always external FQDNs.
	if strings.ContainsAny(target, "*?") {
		return shapeFQDN
	}
	parts := strings.Split(target, ".")
	for _, p := range parts {
		switch p {
		case "svc", "cluster", "local":
			return shapeEndpoint
		}
	}
	// Two-segment "foo-svc" style with the second part not in the
	// in-cluster vocabulary is still external (e.g. api.github.com).
	return shapeFQDN
}
