package parser

import (
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/dormstern/segspec/internal/model"
)

func init() {
	defaultRegistry.Register("pom.xml", parsePomXML)
	defaultRegistry.Register("build.gradle", parseBuildGradle)
	defaultRegistry.Register("build.gradle.kts", parseBuildGradle)
}

// infraLib maps an artifactId pattern to an inferred infrastructure dependency.
type infraLib struct {
	artifactID  string
	target      string
	port        int
	description string
}

var knownLibs = []infraLib{
	{"spring-boot-starter-data-redis", "redis", 6379, "Redis (Spring Boot starter)"},
	{"jedis", "redis", 6379, "Redis (Jedis client)"},
	{"lettuce-core", "redis", 6379, "Redis (Lettuce client)"},
	{"kafka-clients", "kafka", 9092, "Kafka (clients)"},
	{"spring-kafka", "kafka", 9092, "Kafka (Spring)"},
	{"postgresql", "postgresql", 5432, "PostgreSQL"},
	{"spring-boot-starter-data-jpa", "postgresql", 5432, "PostgreSQL (Spring JPA)"},
	{"mysql-connector-java", "mysql", 3306, "MySQL"},
	{"mysql-connector-j", "mysql", 3306, "MySQL"},
	{"mongo-java-driver", "mongodb", 27017, "MongoDB"},
	{"spring-boot-starter-data-mongodb", "mongodb", 27017, "MongoDB (Spring)"},
	{"spring-boot-starter-amqp", "rabbitmq", 5672, "RabbitMQ (Spring AMQP)"},
	{"amqp-client", "rabbitmq", 5672, "RabbitMQ"},
	{"elasticsearch-rest-high-level-client", "elasticsearch", 9200, "Elasticsearch"},
	{"spring-data-elasticsearch", "elasticsearch", 9200, "Elasticsearch (Spring Data)"},
}

// --- pom.xml parser ---

// pomProject is a minimal representation of a Maven POM.
type pomProject struct {
	XMLName      xml.Name        `xml:"project"`
	Dependencies pomDependencies `xml:"dependencies"`
}

type pomDependencies struct {
	Dependency []pomDependency `xml:"dependency"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
}

func parsePomXML(path string) ([]model.NetworkDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var project pomProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("parsing pom.xml: %w", err)
	}

	var deps []model.NetworkDependency
	for _, d := range project.Dependencies.Dependency {
		for _, lib := range knownLibs {
			if d.ArtifactID == lib.artifactID {
				deps = append(deps, model.NetworkDependency{
					Source:      "",
					Target:      lib.target,
					Port:        lib.port,
					Protocol:    "TCP",
					Description: fmt.Sprintf("build dependency: %s:%s -> %s", d.GroupID, d.ArtifactID, lib.description),
					Confidence:  model.Low,
					SourceFile:  path,
				})
				break
			}
		}
	}

	return deps, nil
}

// --- build.gradle parser ---

// Matches lines like: implementation 'group:artifact:version'
// or: implementation "group:artifact:version"
// or: runtimeOnly("group:artifact:version")
var gradleDepRe = regexp.MustCompile(`(?:implementation|compile|runtimeOnly|compileOnly|api)\s*[\(]?\s*['"]([^'"]+)['"]`)

func parseBuildGradle(path string) ([]model.NetworkDependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var deps []model.NetworkDependency
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		matches := gradleDepRe.FindStringSubmatch(trimmed)
		if len(matches) < 2 {
			continue
		}
		depStr := matches[1] // e.g. "org.postgresql:postgresql:42.6.0"
		parts := strings.Split(depStr, ":")
		if len(parts) < 2 {
			continue
		}
		group := parts[0]
		artifact := parts[1]

		for _, lib := range knownLibs {
			if artifact == lib.artifactID {
				deps = append(deps, model.NetworkDependency{
					Source:      "",
					Target:      lib.target,
					Port:        lib.port,
					Protocol:    "TCP",
					Description: fmt.Sprintf("build dependency: %s:%s -> %s", group, artifact, lib.description),
					Confidence:  model.Low,
					SourceFile:  path,
				})
				break
			}
		}
	}

	return deps, nil
}
