package parser

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/dormstern/segspec/internal/model"
	"gopkg.in/yaml.v3"
)

func init() {
	defaultRegistry.Register("*.yaml", parseK8s)
	defaultRegistry.Register("*.yml", parseK8s)
}

// k8sMarker checks whether content looks like a Kubernetes manifest.
func k8sMarker(data []byte) bool {
	return bytes.Contains(data, []byte("apiVersion:")) && bytes.Contains(data, []byte("kind:"))
}

func parseK8s(path string) ([]model.NetworkDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if !k8sMarker(data) {
		return nil, nil
	}

	return parseK8sBytes(data, path)
}

// ParseK8sContent parses K8s manifest YAML content (multi-document) and returns
// discovered network dependencies. Used for parsing helm template output.
// sourceLabel is used as the SourceFile in returned dependencies.
func ParseK8sContent(content string, sourceLabel string) ([]model.NetworkDependency, error) {
	data := []byte(content)

	if !k8sMarker(data) {
		return nil, nil
	}

	return parseK8sBytes(data, sourceLabel)
}

// parseK8sBytes is the shared implementation for parsing K8s manifest bytes.
func parseK8sBytes(data []byte, sourceLabel string) ([]model.NetworkDependency, error) {
	var deps []model.NetworkDependency

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]interface{}
		err := decoder.Decode(&doc)
		if err != nil {
			break // end of documents or parse error
		}
		if doc == nil {
			continue
		}

		kind, _ := doc["kind"].(string)
		switch kind {
		case "Deployment", "StatefulSet":
			deps = append(deps, parseWorkload(doc, sourceLabel)...)
		case "Service":
			deps = append(deps, parseService(doc, sourceLabel)...)
		case "ConfigMap":
			deps = append(deps, parseConfigMap(doc, sourceLabel)...)
		}
	}

	return deps, nil
}

// parseWorkload extracts dependencies from Deployment or StatefulSet manifests.
func parseWorkload(doc map[string]interface{}, path string) []model.NetworkDependency {
	var deps []model.NetworkDependency
	workloadName := metadataName(doc)

	containers := navigateSlice(doc, "spec", "template", "spec", "containers")
	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract container ports (these are ports this workload exposes).
		ports := toSlice(container["ports"])
		for _, p := range ports {
			pm, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			port := toInt(pm["containerPort"])
			if port > 0 {
				deps = append(deps, model.NetworkDependency{
					Source:      workloadName,
					Target:      workloadName,
					Port:        port,
					Protocol:    "TCP",
					Description: fmt.Sprintf("container port %d", port),
					Confidence:  model.High,
					SourceFile:  path,
				})
			}
		}

		// Extract env vars and scan values for URLs/host:port/K8s DNS.
		envVars := toSlice(container["env"])
		for _, e := range envVars {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			envName, _ := em["name"].(string)
			value, _ := em["value"].(string)

			if value != "" {
				deps = append(deps, extractDepsFromValue(value, workloadName, envName, model.High, path)...)
			}

			// Check for valueFrom referencing ConfigMaps or Secrets.
			if vf, ok := em["valueFrom"].(map[string]interface{}); ok {
				if cmRef, ok := vf["configMapKeyRef"].(map[string]interface{}); ok {
					refName, _ := cmRef["name"].(string)
					if refName != "" {
						deps = append(deps, model.NetworkDependency{
							Source:      workloadName,
							Target:      refName,
							Port:        0,
							Protocol:    "TCP",
							Description: fmt.Sprintf("env %s references ConfigMap %s", envName, refName),
							Confidence:  model.Medium,
							SourceFile:  path,
						})
					}
				}
				if secRef, ok := vf["secretKeyRef"].(map[string]interface{}); ok {
					refName, _ := secRef["name"].(string)
					if refName != "" {
						deps = append(deps, model.NetworkDependency{
							Source:      workloadName,
							Target:      refName,
							Port:        0,
							Protocol:    "TCP",
							Description: fmt.Sprintf("env %s references Secret %s", envName, refName),
							Confidence:  model.Medium,
							SourceFile:  path,
						})
					}
				}
			}
		}
	}

	return deps
}

// parseService extracts port information from a Service manifest.
func parseService(doc map[string]interface{}, path string) []model.NetworkDependency {
	var deps []model.NetworkDependency
	svcName := metadataName(doc)

	ports := navigateSlice(doc, "spec", "ports")
	for _, p := range ports {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		port := toInt(pm["port"])
		targetPort := toInt(pm["targetPort"])
		if port > 0 {
			desc := fmt.Sprintf("service port %d", port)
			if targetPort > 0 && targetPort != port {
				desc = fmt.Sprintf("service port %d -> targetPort %d", port, targetPort)
			}
			deps = append(deps, model.NetworkDependency{
				Source:      svcName,
				Target:      svcName,
				Port:        port,
				Protocol:    "TCP",
				Description: desc,
				Confidence:  model.High,
				SourceFile:  path,
			})
		}
	}

	return deps
}

// parseConfigMap scans ConfigMap data values for URLs and host:port patterns.
func parseConfigMap(doc map[string]interface{}, path string) []model.NetworkDependency {
	var deps []model.NetworkDependency
	cmName := metadataName(doc)

	data, ok := navigateMap(doc, "data")
	if !ok {
		return nil
	}

	for key, val := range data {
		str, ok := val.(string)
		if !ok {
			continue
		}
		deps = append(deps, extractDepsFromValue(str, cmName, key, model.Medium, path)...)
	}

	return deps
}

// --- helpers ---

func metadataName(doc map[string]interface{}) string {
	meta, _ := doc["metadata"].(map[string]interface{})
	if meta == nil {
		return "unknown"
	}
	name, _ := meta["name"].(string)
	if name == "" {
		return "unknown"
	}
	return name
}

func navigateMap(doc map[string]interface{}, keys ...string) (map[string]interface{}, bool) {
	current := doc
	for _, k := range keys {
		next, ok := current[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func navigateSlice(doc map[string]interface{}, keys ...string) []interface{} {
	if len(keys) == 0 {
		return nil
	}
	current := doc
	for _, k := range keys[:len(keys)-1] {
		next, ok := current[k].(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return toSlice(current[keys[len(keys)-1]])
}

func toSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	s, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return s
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

var (
	// Matches host:port where port is numeric.
	hostPortRe = regexp.MustCompile(`([a-zA-Z0-9][-a-zA-Z0-9.]*):(\d{2,5})`)
	// Matches K8s internal DNS: <host>.<ns>.svc.cluster.local
	// Host may contain dots (e.g. pod-0.svc-headless) so we use a broader match.
	k8sDNSRe = regexp.MustCompile(`([a-zA-Z0-9][-a-zA-Z0-9.]*)\.([a-zA-Z0-9][-a-zA-Z0-9]*)\.svc\.cluster\.local`)
)

// extractDepsFromValue scans a string value for URLs, host:port, and K8s DNS patterns.
func extractDepsFromValue(value, source, context string, confidence model.Confidence, path string) []model.NetworkDependency {
	var deps []model.NetworkDependency

	// Try parsing as URL first.
	if u, err := url.Parse(value); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "postgresql" || u.Scheme == "postgres" || u.Scheme == "redis" || u.Scheme == "amqp" || u.Scheme == "mongodb" || u.Scheme == "mysql" || u.Scheme == "kafka") {
		host := u.Hostname()
		port := 0
		if u.Port() != "" {
			port, _ = strconv.Atoi(u.Port())
		}
		deps = append(deps, model.NetworkDependency{
			Source:      source,
			Target:      host,
			Port:        port,
			Protocol:    "TCP",
			Description: fmt.Sprintf("%s: URL %s", context, value),
			Confidence:  confidence,
			SourceFile:  path,
		})
		return deps
	}

	// Check for K8s DNS patterns.
	if matches := k8sDNSRe.FindStringSubmatch(value); len(matches) == 3 {
		svc := matches[1]
		ns := matches[2]
		target := fmt.Sprintf("%s.%s", svc, ns)
		port := 0
		// Also extract port if present after the DNS name.
		if hpMatches := hostPortRe.FindStringSubmatch(value); len(hpMatches) == 3 {
			port, _ = strconv.Atoi(hpMatches[2])
		}
		deps = append(deps, model.NetworkDependency{
			Source:      source,
			Target:      target,
			Port:        port,
			Protocol:    "TCP",
			Description: fmt.Sprintf("%s: K8s service DNS %s", context, value),
			Confidence:  confidence,
			SourceFile:  path,
		})
		return deps
	}

	// Check for host:port patterns.
	if matches := hostPortRe.FindAllStringSubmatch(value, -1); len(matches) > 0 {
		for _, m := range matches {
			host := m[1]
			port, _ := strconv.Atoi(m[2])
			// Filter out obviously non-network patterns.
			if strings.HasPrefix(host, "sha") || port < 1 || port > 65535 {
				continue
			}
			deps = append(deps, model.NetworkDependency{
				Source:      source,
				Target:      host,
				Port:        port,
				Protocol:    "TCP",
				Description: fmt.Sprintf("%s: host:port %s:%d", context, host, port),
				Confidence:  confidence,
				SourceFile:  path,
			})
		}
	}

	return deps
}
