package parser

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dormstern/segspec/internal/model"
	"gopkg.in/yaml.v3"
)

func init() {
	defaultRegistry.Register("docker-compose.yml", parseCompose)
	defaultRegistry.Register("docker-compose.yaml", parseCompose)
	defaultRegistry.Register("compose.yml", parseCompose)
	defaultRegistry.Register("compose.yaml", parseCompose)
}

// wellKnownImages maps image name prefixes to their default port and description.
var wellKnownImages = map[string]struct {
	port int
	desc string
}{
	"postgres":      {5432, "PostgreSQL"},
	"mysql":         {3306, "MySQL"},
	"mariadb":       {3306, "MariaDB"},
	"redis":         {6379, "Redis"},
	"mongo":         {27017, "MongoDB"},
	"rabbitmq":      {5672, "RabbitMQ"},
	"elasticsearch": {9200, "Elasticsearch"},
	"kafka":         {9092, "Kafka"},
	"nats":          {4222, "NATS"},
	"memcached":     {11211, "Memcached"},
	"consul":        {8500, "Consul"},
	"etcd":          {2379, "etcd"},
	"zookeeper":     {2181, "ZooKeeper"},
	"minio":         {9000, "MinIO"},
	"vault":         {8200, "Vault"},
}

// composeFile represents a docker-compose YAML structure.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string      `yaml:"image"`
	Ports       []string    `yaml:"ports"`
	DependsOn   interface{} `yaml:"depends_on"`
	Environment interface{} `yaml:"environment"`
}

func parseCompose(path string) ([]model.NetworkDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing compose %s: %w", path, err)
	}

	var deps []model.NetworkDependency

	for serviceName, svc := range cf.Services {
		// Ports: exposed ports for this service
		for _, p := range svc.Ports {
			containerPort := parseContainerPort(p)
			if containerPort > 0 {
				deps = append(deps, model.NetworkDependency{
					Source:      serviceName,
					Target:      serviceName,
					Port:        containerPort,
					Protocol:    "TCP",
					Description: "exposed port",
					Confidence:  model.High,
					SourceFile:  path,
				})
			}
		}

		// DependsOn: service dependencies
		depNames := parseDependsOn(svc.DependsOn)
		for _, depName := range depNames {
			dep := model.NetworkDependency{
				Source:      serviceName,
				Target:      depName,
				Protocol:    "TCP",
				Description: "depends_on",
				Confidence:  model.Medium,
				SourceFile:  path,
			}
			// Try to infer port from the dependent service's image
			if depSvc, ok := cf.Services[depName]; ok {
				if port, desc := inferFromImage(depSvc.Image); port > 0 {
					dep.Port = port
					dep.Description = desc
					dep.Confidence = model.High
				}
			}
			deps = append(deps, dep)
		}

		// Image: infer well-known service ports
		if port, desc := inferFromImage(svc.Image); port > 0 {
			deps = append(deps, model.NetworkDependency{
				Source:      serviceName,
				Target:      serviceName,
				Port:        port,
				Protocol:    "TCP",
				Description: desc + " (inferred from image)",
				Confidence:  model.Low,
				SourceFile:  path,
			})
		}

		// Environment: scan for URLs/connection strings
		envVars := parseEnvironment(svc.Environment)
		for _, val := range envVars {
			if d, ok := extractFromValue(val, path); ok {
				d.Source = serviceName
				if d.Confidence == model.High {
					d.Confidence = model.Medium
				}
				deps = append(deps, d)
			}
		}
	}

	return deps, nil
}

// parseContainerPort extracts the container port from a port mapping string.
// Supports formats: "8080", "8080:80", "127.0.0.1:8080:80"
func parseContainerPort(s string) int {
	s = strings.TrimSpace(s)
	// Remove protocol suffix like /tcp or /udp
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ":")
	// Last part is always the container port
	portStr := parts[len(parts)-1]
	// Handle range like 8080-8081
	if idx := strings.Index(portStr, "-"); idx >= 0 {
		portStr = portStr[:idx]
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// parseDependsOn handles both list and map forms of depends_on.
func parseDependsOn(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []interface{}:
		var names []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				names = append(names, s)
			}
		}
		return names
	case map[string]interface{}:
		var names []string
		for name := range val {
			names = append(names, name)
		}
		return names
	}
	return nil
}

// inferFromImage checks if an image name matches a well-known service.
func inferFromImage(image string) (int, string) {
	if image == "" {
		return 0, ""
	}
	// Strip registry prefix and tag: e.g. "docker.io/library/postgres:15" -> "postgres"
	name := image
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.Index(name, ":"); idx >= 0 {
		name = name[:idx]
	}
	name = strings.ToLower(name)

	// Check exact match first
	if info, ok := wellKnownImages[name]; ok {
		return info.port, info.desc
	}
	// Check prefix match (e.g., "bitnami/redis" already stripped to "redis")
	for prefix, info := range wellKnownImages {
		if strings.HasPrefix(name, prefix) {
			return info.port, info.desc
		}
	}
	return 0, ""
}

// parseEnvironment handles both list (KEY=VAL) and map forms.
func parseEnvironment(v interface{}) map[string]string {
	result := make(map[string]string)
	if v == nil {
		return result
	}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok {
				if idx := strings.Index(s, "="); idx >= 0 {
					result[s[:idx]] = s[idx+1:]
				}
			}
		}
	case map[string]interface{}:
		for k, vv := range val {
			if s, ok := vv.(string); ok {
				result[k] = s
			}
		}
	}
	return result
}
