// Package netpol parses existing Kubernetes NetworkPolicy and Cilium
// CiliumNetworkPolicy / CiliumClusterwideNetworkPolicy YAML into the flat
// validator.Policy shape consumed by the static checks.
//
// This is a read-only adapter — it does NOT model the full NP / CNP
// surface, only the fields the four validate checks inspect. New checks
// add fields here, not in the validator package.
package netpol

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dormstern/segspec/internal/validator"
)

// ParseResult is what ReadPath returns — both the parsed policies (used by
// every check) and any workload-label projections found alongside (used by
// the unreferenced-selector cross-check).
type ParseResult struct {
	Policies  []validator.Policy
	Workloads []validator.WorkloadLabels
	// Files is the count of YAML files we successfully opened. Useful for
	// the "no policies found" silent-success case.
	Files int
}

// ReadPath walks the given path and returns every NetworkPolicy /
// CiliumNetworkPolicy / CiliumClusterwideNetworkPolicy document it can
// parse, plus any Deployment / DaemonSet / StatefulSet / Pod for the
// unreferenced-selector cross-check.
//
// If path is a regular file it is parsed directly. If path is a directory
// every .yaml / .yml under it is walked. If path is "-" stdin is read.
func ReadPath(path string) (ParseResult, error) {
	var pr ParseResult

	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return pr, fmt.Errorf("read stdin: %w", err)
		}
		return parseBytes("<stdin>", data)
	}

	info, err := os.Stat(path)
	if err != nil {
		return pr, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return pr, err
		}
		return parseBytes(path, data)
	}

	walkErr := filepath.Walk(path, func(p string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil // skip unreadable, do not fail the whole walk
		}
		sub, perr := parseBytes(p, data)
		if perr != nil {
			return nil // skip un-parseable file, do not fail the whole walk
		}
		pr.Policies = append(pr.Policies, sub.Policies...)
		pr.Workloads = append(pr.Workloads, sub.Workloads...)
		pr.Files += sub.Files
		return nil
	})
	if walkErr != nil {
		return pr, walkErr
	}
	return pr, nil
}

// parseBytes splits a multi-document YAML stream and routes each document
// to the right extractor. Unknown kinds are ignored — they're not an
// error, just not interesting to the validator.
func parseBytes(file string, data []byte) (ParseResult, error) {
	var pr ParseResult
	pr.Files = 1
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err != nil {
			if err == io.EOF {
				break
			}
			return pr, err
		}
		// Decoded document root is a DocumentNode wrapping a MappingNode.
		root := documentRoot(&node)
		if root == nil {
			continue
		}
		kind := stringField(root, "kind")
		switch kind {
		case "NetworkPolicy":
			if pol, ok := parseK8sNetworkPolicy(file, root); ok {
				pr.Policies = append(pr.Policies, pol)
			}
		case "CiliumNetworkPolicy", "CiliumClusterwideNetworkPolicy":
			if pol, ok := parseCiliumNetworkPolicy(file, root); ok {
				pr.Policies = append(pr.Policies, pol)
			}
		case "Deployment", "DaemonSet", "StatefulSet", "Pod", "ReplicaSet":
			if w, ok := parseWorkload(root); ok {
				pr.Workloads = append(pr.Workloads, w)
			}
		}
	}
	return pr, nil
}

func documentRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

// stringField returns m[key] as a string when m is a MappingNode and the
// value is a scalar.
func stringField(m *yaml.Node, key string) string {
	v := mapField(m, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// mapField returns the value node for the given key in a MappingNode, or
// nil if the key is absent or the input is not a map.
func mapField(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// parseK8sNetworkPolicy extracts the validator.Policy view of a vanilla
// networking.k8s.io/v1 NetworkPolicy.
func parseK8sNetworkPolicy(file string, root *yaml.Node) (validator.Policy, bool) {
	pol := validator.Policy{
		APIVersion: stringField(root, "apiVersion"),
		Kind:       stringField(root, "kind"),
		File:       file,
		Line:       root.Line,
	}
	if md := mapField(root, "metadata"); md != nil {
		pol.Name = stringField(md, "name")
		pol.Namespace = stringField(md, "namespace")
	}
	spec := mapField(root, "spec")
	if spec == nil {
		return pol, true
	}
	pol.PodSelector = labelPairsFromSelector(mapField(spec, "podSelector"))

	if eg := mapField(spec, "egress"); eg != nil && eg.Kind == yaml.SequenceNode {
		for _, item := range eg.Content {
			pol.Egress = append(pol.Egress, parseK8sEgress(item))
		}
	}
	if ing := mapField(spec, "ingress"); ing != nil && ing.Kind == yaml.SequenceNode {
		for _, item := range ing.Content {
			pol.Ingress = append(pol.Ingress, parseK8sIngress(item))
		}
	}
	return pol, true
}

func parseK8sEgress(item *yaml.Node) validator.EgressRule {
	r := validator.EgressRule{Line: item.Line}
	if ports := mapField(item, "ports"); ports != nil && ports.Kind == yaml.SequenceNode {
		r.HasToPorts = len(ports.Content) > 0
		for _, p := range ports.Content {
			if portNode := mapField(p, "port"); portNode != nil && portNode.Kind == yaml.ScalarNode {
				if n := parseIntScalar(portNode.Value); n > 0 {
					r.ToPortsPorts = append(r.ToPortsPorts, n)
				}
			}
		}
	}
	if to := mapField(item, "to"); to != nil && to.Kind == yaml.SequenceNode {
		for _, peer := range to.Content {
			ps := validator.PeerSelector{Line: peer.Line}
			ps.PodSelector = labelPairsFromSelector(mapField(peer, "podSelector"))
			r.To = append(r.To, ps)
		}
	}
	return r
}

func parseK8sIngress(item *yaml.Node) validator.IngressRule {
	r := validator.IngressRule{Line: item.Line}
	if from := mapField(item, "from"); from != nil && from.Kind == yaml.SequenceNode {
		for _, peer := range from.Content {
			ps := validator.PeerSelector{Line: peer.Line}
			ps.PodSelector = labelPairsFromSelector(mapField(peer, "podSelector"))
			r.From = append(r.From, ps)
		}
	}
	return r
}

// parseCiliumNetworkPolicy handles the Cilium variants. Cilium's spec uses
// `endpointSelector` instead of `podSelector` and adds `toFQDNs` /
// `toEntities` to egress.
func parseCiliumNetworkPolicy(file string, root *yaml.Node) (validator.Policy, bool) {
	pol := validator.Policy{
		APIVersion: stringField(root, "apiVersion"),
		Kind:       stringField(root, "kind"),
		File:       file,
		Line:       root.Line,
	}
	if md := mapField(root, "metadata"); md != nil {
		pol.Name = stringField(md, "name")
		pol.Namespace = stringField(md, "namespace")
	}
	spec := mapField(root, "spec")
	if spec == nil {
		return pol, true
	}
	pol.PodSelector = labelPairsFromSelector(mapField(spec, "endpointSelector"))

	if eg := mapField(spec, "egress"); eg != nil && eg.Kind == yaml.SequenceNode {
		for _, item := range eg.Content {
			pol.Egress = append(pol.Egress, parseCiliumEgress(item))
		}
	}
	return pol, true
}

func parseCiliumEgress(item *yaml.Node) validator.EgressRule {
	r := validator.EgressRule{Line: item.Line}

	if tp := mapField(item, "toPorts"); tp != nil && tp.Kind == yaml.SequenceNode {
		r.HasToPorts = len(tp.Content) > 0
		for _, blk := range tp.Content {
			if ports := mapField(blk, "ports"); ports != nil && ports.Kind == yaml.SequenceNode {
				for _, p := range ports.Content {
					if portNode := mapField(p, "port"); portNode != nil && portNode.Kind == yaml.ScalarNode {
						if n := parseIntScalar(portNode.Value); n > 0 {
							r.ToPortsPorts = append(r.ToPortsPorts, n)
						}
					}
				}
			}
		}
	}

	if fq := mapField(item, "toFQDNs"); fq != nil && fq.Kind == yaml.SequenceNode {
		for _, f := range fq.Content {
			r.ToFQDNs = append(r.ToFQDNs, validator.FQDNTarget{
				MatchName:    stringField(f, "matchName"),
				MatchPattern: stringField(f, "matchPattern"),
				Line:         f.Line,
			})
		}
	}

	if te := mapField(item, "toEntities"); te != nil && te.Kind == yaml.SequenceNode {
		for _, e := range te.Content {
			if e.Kind == yaml.ScalarNode {
				r.ToEntities = append(r.ToEntities, e.Value)
			}
		}
	}

	return r
}

// labelPairsFromSelector flattens a LabelSelector into key/value pairs. We
// only walk matchLabels — matchExpressions don't carry a single
// key/value-with-line that the OversizedLabel check can point at, and an
// empty selector returns an empty slice (which Run treats as "select all,
// not unreferenced").
func labelPairsFromSelector(sel *yaml.Node) []validator.LabelPair {
	if sel == nil {
		return nil
	}
	var out []validator.LabelPair
	if ml := mapField(sel, "matchLabels"); ml != nil && ml.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(ml.Content); i += 2 {
			k := ml.Content[i]
			v := ml.Content[i+1]
			if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
				continue
			}
			out = append(out, validator.LabelPair{Key: k.Value, Value: v.Value, Line: k.Line})
		}
	}
	return out
}

// parseWorkload extracts namespace + pod-template labels from a workload
// manifest so the unreferenced-selector check has ground truth.
func parseWorkload(root *yaml.Node) (validator.WorkloadLabels, bool) {
	w := validator.WorkloadLabels{Labels: map[string]string{}}
	if md := mapField(root, "metadata"); md != nil {
		w.Namespace = stringField(md, "namespace")
	}
	spec := mapField(root, "spec")
	if spec == nil {
		return w, false
	}
	// Workload pod-template labels live at .spec.template.metadata.labels;
	// a bare Pod stores labels at .metadata.labels.
	tpl := mapField(spec, "template")
	var labelHost *yaml.Node
	if tpl != nil {
		if md := mapField(tpl, "metadata"); md != nil {
			labelHost = mapField(md, "labels")
		}
	} else {
		// Pod / no template — fall back to top-level metadata.labels.
		if md := mapField(root, "metadata"); md != nil {
			labelHost = mapField(md, "labels")
		}
	}
	if labelHost == nil || labelHost.Kind != yaml.MappingNode {
		return w, false
	}
	for i := 0; i+1 < len(labelHost.Content); i += 2 {
		k := labelHost.Content[i]
		v := labelHost.Content[i+1]
		if k.Kind == yaml.ScalarNode && v.Kind == yaml.ScalarNode {
			w.Labels[k.Value] = v.Value
		}
	}
	return w, len(w.Labels) > 0
}

// parseIntScalar returns 0 on any error — callers treat 0 as "no port",
// which is fine because port 0 is reserved.
func parseIntScalar(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
