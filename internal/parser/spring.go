package parser

import (
	"bufio"
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
	defaultRegistry.Register("application.yml", parseSpringYAML)
	defaultRegistry.Register("application.yaml", parseSpringYAML)
	defaultRegistry.Register("application.properties", parseSpringProperties)
}

// jdbcPattern matches JDBC URLs like jdbc:postgresql://host:port/db or jdbc:postgresql://host/db
var jdbcPattern = regexp.MustCompile(`^jdbc:(\w+)://([^/:]+)(?::(\d+))?`)

// hostPortPattern matches host:port where port is numeric
var hostPortPattern = regexp.MustCompile(`([a-zA-Z0-9][-a-zA-Z0-9_.]+):(\d{2,5})`)

// urlPattern matches protocol://host:port URLs
var urlPattern = regexp.MustCompile(`^https?://([a-zA-Z0-9][-a-zA-Z0-9_.]+):(\d+)`)

// jdbcDescriptions maps JDBC driver names to human-readable descriptions.
var jdbcDescriptions = map[string]string{
	"postgresql": "PostgreSQL",
	"postgres":   "PostgreSQL",
	"mysql":      "MySQL",
	"mariadb":    "MariaDB",
	"sqlserver":  "SQL Server",
	"oracle":     "Oracle",
	"h2":         "H2",
}

// springConfig represents the subset of Spring application.yml we care about.
type springConfig struct {
	Spring struct {
		Datasource struct {
			URL string `yaml:"url"`
		} `yaml:"datasource"`
		Redis struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"redis"`
		Data struct {
			Redis struct {
				Host string `yaml:"host"`
				Port int    `yaml:"port"`
			} `yaml:"redis"`
		} `yaml:"data"`
		Kafka struct {
			BootstrapServers string `yaml:"bootstrap-servers"`
		} `yaml:"kafka"`
		RabbitMQ struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"rabbitmq"`
	} `yaml:"spring"`
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`
}

func parseSpringYAML(path string) ([]model.NetworkDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var allDeps []model.NetworkDependency

	// Use yaml.NewDecoder to read ALL documents separated by ---
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var cfg springConfig
		err := decoder.Decode(&cfg)
		if err != nil {
			break // end of documents or parse error
		}

		deps := extractSpringDeps(cfg, path)
		allDeps = mergeUnique(allDeps, deps)
	}

	// Also scan all string values in the raw YAML documents for URL patterns not caught above
	rawDecoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var raw map[string]interface{}
		err := rawDecoder.Decode(&raw)
		if err != nil {
			break
		}
		if raw == nil {
			continue
		}
		found := extractURLsFromMap(raw, path)
		allDeps = mergeUnique(allDeps, found)
	}

	return allDeps, nil
}

// extractSpringDeps extracts dependencies from a single parsed springConfig document.
func extractSpringDeps(cfg springConfig, path string) []model.NetworkDependency {
	var deps []model.NetworkDependency

	// Datasource URL (JDBC)
	if cfg.Spring.Datasource.URL != "" {
		if d, ok := parseJDBC(cfg.Spring.Datasource.URL, path); ok {
			deps = append(deps, d)
		}
	}

	// Redis (Spring Boot 2.x: spring.redis.host)
	if cfg.Spring.Redis.Host != "" {
		port := cfg.Spring.Redis.Port
		if port == 0 {
			port = 6379
		}
		deps = append(deps, model.NetworkDependency{
			Target:      cfg.Spring.Redis.Host,
			Port:        port,
			Protocol:    "TCP",
			Description: "Redis",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	// Redis (Spring Boot 3.x: spring.data.redis.host)
	if cfg.Spring.Data.Redis.Host != "" {
		port := cfg.Spring.Data.Redis.Port
		if port == 0 {
			port = 6379
		}
		deps = append(deps, model.NetworkDependency{
			Target:      cfg.Spring.Data.Redis.Host,
			Port:        port,
			Protocol:    "TCP",
			Description: "Redis",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	// Kafka bootstrap servers
	if cfg.Spring.Kafka.BootstrapServers != "" {
		for _, broker := range parseKafkaBrokers(cfg.Spring.Kafka.BootstrapServers) {
			deps = append(deps, model.NetworkDependency{
				Target:      broker.host,
				Port:        broker.port,
				Protocol:    "TCP",
				Description: "Kafka",
				Confidence:  model.High,
				SourceFile:  path,
			})
		}
	}

	// RabbitMQ
	if cfg.Spring.RabbitMQ.Host != "" {
		port := cfg.Spring.RabbitMQ.Port
		if port == 0 {
			port = 5672
		}
		deps = append(deps, model.NetworkDependency{
			Target:      cfg.Spring.RabbitMQ.Host,
			Port:        port,
			Protocol:    "TCP",
			Description: "RabbitMQ",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	// Server port (the app's own listening port)
	if cfg.Server.Port != 0 {
		deps = append(deps, model.NetworkDependency{
			Target:      "self",
			Port:        cfg.Server.Port,
			Protocol:    "TCP",
			Description: "server listening port",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	return deps
}

func parseSpringProperties(path string) ([]model.NetworkDependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		// Split on first = or :
		idx := strings.IndexAny(line, "=:")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		props[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", path, err)
	}

	var deps []model.NetworkDependency

	// Datasource URL
	if v, ok := props["spring.datasource.url"]; ok {
		if d, found := parseJDBC(v, path); found {
			deps = append(deps, d)
		}
	}

	// Redis
	if host, ok := props["spring.redis.host"]; ok {
		port := 6379
		if p, ok := props["spring.redis.port"]; ok {
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
		deps = append(deps, model.NetworkDependency{
			Target:      host,
			Port:        port,
			Protocol:    "TCP",
			Description: "Redis",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	// Kafka
	if v, ok := props["spring.kafka.bootstrap-servers"]; ok {
		for _, broker := range parseKafkaBrokers(v) {
			deps = append(deps, model.NetworkDependency{
				Target:      broker.host,
				Port:        broker.port,
				Protocol:    "TCP",
				Description: "Kafka",
				Confidence:  model.High,
				SourceFile:  path,
			})
		}
	}

	// RabbitMQ
	if host, ok := props["spring.rabbitmq.host"]; ok {
		port := 5672
		if p, ok := props["spring.rabbitmq.port"]; ok {
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
		deps = append(deps, model.NetworkDependency{
			Target:      host,
			Port:        port,
			Protocol:    "TCP",
			Description: "RabbitMQ",
			Confidence:  model.High,
			SourceFile:  path,
		})
	}

	// Server port
	if v, ok := props["server.port"]; ok {
		if port, err := strconv.Atoi(v); err == nil {
			deps = append(deps, model.NetworkDependency{
				Target:      "self",
				Port:        port,
				Protocol:    "TCP",
				Description: "server listening port",
				Confidence:  model.High,
				SourceFile:  path,
			})
		}
	}

	// Scan all property values for URL patterns
	for key, val := range props {
		// Skip keys we've already handled explicitly
		if isHandledSpringKey(key) {
			continue
		}
		if d, ok := extractFromValue(val, path); ok {
			deps = mergeUnique(deps, []model.NetworkDependency{d})
		}
	}

	return deps, nil
}

func isHandledSpringKey(key string) bool {
	handled := []string{
		"spring.datasource.url",
		"spring.redis.host", "spring.redis.port",
		"spring.kafka.bootstrap-servers",
		"spring.rabbitmq.host", "spring.rabbitmq.port",
		"server.port",
	}
	for _, h := range handled {
		if key == h {
			return true
		}
	}
	return false
}

type hostPort struct {
	host string
	port int
}

func parseKafkaBrokers(s string) []hostPort {
	var result []hostPort
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		matches := hostPortPattern.FindStringSubmatch(part)
		if matches != nil {
			port, _ := strconv.Atoi(matches[2])
			result = append(result, hostPort{host: matches[1], port: port})
		}
	}
	return result
}

// jdbcDefaultPorts maps JDBC driver names to their default ports.
var jdbcDefaultPorts = map[string]int{
	"postgresql": 5432,
	"postgres":   5432,
	"mysql":      3306,
	"mariadb":    3306,
	"oracle":     1521,
	"sqlserver":  1433,
	"mssql":      1433,
}

// jdbcSkipDrivers lists JDBC drivers that are in-memory/embedded and not network deps.
var jdbcSkipDrivers = map[string]bool{
	"h2":    true,
	"derby": true,
}

func parseJDBC(raw, sourceFile string) (model.NetworkDependency, bool) {
	matches := jdbcPattern.FindStringSubmatch(raw)
	if matches == nil {
		return model.NetworkDependency{}, false
	}
	driver := strings.ToLower(matches[1])
	host := matches[2]

	// Skip in-memory/embedded databases
	if jdbcSkipDrivers[driver] {
		return model.NetworkDependency{}, false
	}

	confidence := model.High
	port := 0
	if matches[3] != "" {
		port, _ = strconv.Atoi(matches[3])
	} else {
		// Port not specified — use default for the driver
		defaultPort, hasDefault := jdbcDefaultPorts[driver]
		if !hasDefault {
			// Unknown driver with no port — can't determine the port
			return model.NetworkDependency{}, false
		}
		port = defaultPort
		confidence = model.Medium
	}

	desc := driver
	if d, ok := jdbcDescriptions[driver]; ok {
		desc = d
	}
	return model.NetworkDependency{
		Target:      host,
		Port:        port,
		Protocol:    "TCP",
		Description: desc,
		Confidence:  confidence,
		SourceFile:  sourceFile,
	}, true
}

// extractFromValue tries to parse a single string value as a URL or host:port.
func extractFromValue(val, sourceFile string) (model.NetworkDependency, bool) {
	// Try JDBC first
	if d, ok := parseJDBC(val, sourceFile); ok {
		return d, true
	}

	// Try URL (http/https)
	if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		if u, err := url.Parse(val); err == nil && u.Host != "" {
			host := u.Hostname()
			port := 0
			if p := u.Port(); p != "" {
				port, _ = strconv.Atoi(p)
			} else if u.Scheme == "https" {
				port = 443
			} else {
				port = 80
			}
			return model.NetworkDependency{
				Target:      host,
				Port:        port,
				Protocol:    "TCP",
				Description: "HTTP service",
				Confidence:  model.High,
				SourceFile:  sourceFile,
			}, true
		}
	}

	// Try bare host:port
	matches := hostPortPattern.FindStringSubmatch(val)
	if matches != nil {
		port, _ := strconv.Atoi(matches[2])
		return model.NetworkDependency{
			Target:      matches[1],
			Port:        port,
			Protocol:    "TCP",
			Description: "network service",
			Confidence:  model.Medium,
			SourceFile:  sourceFile,
		}, true
	}

	return model.NetworkDependency{}, false
}

// extractURLsFromMap walks a YAML map and returns deps from URL-like string values.
func extractURLsFromMap(m map[string]interface{}, sourceFile string) []model.NetworkDependency {
	var deps []model.NetworkDependency
	walkYAML(m, func(val string) {
		if d, ok := extractFromValue(val, sourceFile); ok {
			deps = append(deps, d)
		}
	})
	return deps
}

func walkYAML(v interface{}, fn func(string)) {
	switch val := v.(type) {
	case map[string]interface{}:
		for _, child := range val {
			walkYAML(child, fn)
		}
	case []interface{}:
		for _, child := range val {
			walkYAML(child, fn)
		}
	case string:
		fn(val)
	}
}

// mergeUnique appends deps from extra that don't already exist in base (by Target+Port).
func mergeUnique(base, extra []model.NetworkDependency) []model.NetworkDependency {
	seen := make(map[string]bool)
	for _, d := range base {
		seen[fmt.Sprintf("%s:%d", d.Target, d.Port)] = true
	}
	for _, d := range extra {
		key := fmt.Sprintf("%s:%d", d.Target, d.Port)
		if !seen[key] {
			seen[key] = true
			base = append(base, d)
		}
	}
	return base
}
