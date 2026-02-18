package parser

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

func init() {
	defaultRegistry.Register(".env", parseEnvFile)
}

// wellKnownEnvVars maps env var name patterns to descriptions.
// A key ending with "*" matches as a prefix; otherwise exact match.
var wellKnownEnvVars = map[string]string{
	"DATABASE_URL":       "database",
	"DB_URL":             "database",
	"DB_HOST":            "database",
	"REDIS_URL":          "Redis",
	"REDIS_HOST":         "Redis",
	"KAFKA_BROKERS":      "Kafka",
	"KAFKA_BOOTSTRAP":    "Kafka",
	"RABBITMQ_URL":       "RabbitMQ",
	"RABBITMQ_HOST":      "RabbitMQ",
	"AMQP_URL":           "RabbitMQ",
	"MONGODB_URI":        "MongoDB",
	"MONGODB_URL":        "MongoDB",
	"MONGO_URL":          "MongoDB",
	"MONGO_HOST":         "MongoDB",
	"ELASTICSEARCH_URL":  "Elasticsearch",
	"ELASTICSEARCH_HOST": "Elasticsearch",
	"API_URL":            "API service",
	"SERVICE_URL":        "service",
	"POSTGRES_HOST":      "PostgreSQL",
	"POSTGRES_URL":       "PostgreSQL",
	"MYSQL_HOST":         "MySQL",
	"MYSQL_URL":          "MySQL",
	"NATS_URL":           "NATS",
	"MEMCACHED_HOST":     "Memcached",
	"CONSUL_HTTP_ADDR":   "Consul",
	"VAULT_ADDR":         "Vault",
}

func parseEnvFile(path string) ([]model.NetworkDependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	var deps []model.NetworkDependency
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// Strip surrounding quotes
		val = stripQuotes(val)

		if val == "" {
			continue
		}

		// Check if key is well-known
		desc := matchWellKnownEnv(key)

		// Try to extract host:port from value
		d, ok := extractFromValue(val, path)
		if !ok {
			continue
		}

		// Override description if we have a well-known match
		if desc != "" {
			d.Description = desc
		}

		// Env vars are medium confidence
		d.Confidence = model.Medium

		dedup := fmt.Sprintf("%s:%d", d.Target, d.Port)
		if seen[dedup] {
			continue
		}
		seen[dedup] = true
		deps = append(deps, d)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", path, err)
	}

	return deps, nil
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func matchWellKnownEnv(key string) string {
	upper := strings.ToUpper(key)
	// Exact match
	if desc, ok := wellKnownEnvVars[upper]; ok {
		return desc
	}
	// Suffix-based heuristics
	suffixes := map[string]string{
		"_URL":  "service",
		"_URI":  "service",
		"_HOST": "service",
		"_ADDR": "service",
	}
	for suffix, fallback := range suffixes {
		if strings.HasSuffix(upper, suffix) {
			return fallback
		}
	}
	return ""
}
