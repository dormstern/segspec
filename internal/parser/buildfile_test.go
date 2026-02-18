package parser

import (
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestPomXMLMultipleDependencies(t *testing.T) {
	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>order-service</artifactId>
  <version>1.0.0</version>

  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-redis</artifactId>
    </dependency>
    <dependency>
      <groupId>org.postgresql</groupId>
      <artifactId>postgresql</artifactId>
      <version>42.6.0</version>
    </dependency>
    <dependency>
      <groupId>org.apache.kafka</groupId>
      <artifactId>kafka-clients</artifactId>
      <version>3.6.0</version>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-test</artifactId>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`

	path := writeTempFile(t, "pom.xml", pom)
	deps, err := parsePomXML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3: redis, postgresql, kafka (spring-boot-starter-web and test dep are ignored)
	assertDepCount(t, deps, 3)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "redis" && d.Port == 6379 && d.Confidence == model.Low
	}, "Redis from pom.xml")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "postgresql" && d.Port == 5432 && d.Confidence == model.Low
	}, "PostgreSQL from pom.xml")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "kafka" && d.Port == 9092 && d.Confidence == model.Low
	}, "Kafka from pom.xml")

	// All should have Protocol TCP
	for _, d := range deps {
		if d.Protocol != "TCP" {
			t.Errorf("expected Protocol TCP, got %q", d.Protocol)
		}
	}
}

func TestPomXMLAllKnownLibs(t *testing.T) {
	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <dependencies>
    <dependency>
      <groupId>redis.clients</groupId>
      <artifactId>jedis</artifactId>
    </dependency>
    <dependency>
      <groupId>io.lettuce</groupId>
      <artifactId>lettuce-core</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.kafka</groupId>
      <artifactId>spring-kafka</artifactId>
    </dependency>
    <dependency>
      <groupId>org.postgresql</groupId>
      <artifactId>postgresql</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
    </dependency>
    <dependency>
      <groupId>mysql</groupId>
      <artifactId>mysql-connector-java</artifactId>
    </dependency>
    <dependency>
      <groupId>com.mysql</groupId>
      <artifactId>mysql-connector-j</artifactId>
    </dependency>
    <dependency>
      <groupId>org.mongodb</groupId>
      <artifactId>mongo-java-driver</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-mongodb</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-amqp</artifactId>
    </dependency>
    <dependency>
      <groupId>com.rabbitmq</groupId>
      <artifactId>amqp-client</artifactId>
    </dependency>
    <dependency>
      <groupId>org.elasticsearch.client</groupId>
      <artifactId>elasticsearch-rest-high-level-client</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.data</groupId>
      <artifactId>spring-data-elasticsearch</artifactId>
    </dependency>
  </dependencies>
</project>`

	path := writeTempFile(t, "pom.xml", pom)
	deps, err := parsePomXML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 13 known libraries
	assertDepCount(t, deps, 13)

	tests := []struct {
		target string
		port   int
	}{
		{"redis", 6379},
		{"kafka", 9092},
		{"postgresql", 5432},
		{"mysql", 3306},
		{"mongodb", 27017},
		{"rabbitmq", 5672},
		{"elasticsearch", 9200},
	}
	for _, tt := range tests {
		assertHasDep(t, deps, func(d model.NetworkDependency) bool {
			return d.Target == tt.target && d.Port == tt.port
		}, tt.target)
	}
}

func TestPomXMLUnknownDepsIgnored(t *testing.T) {
	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>com.fasterxml.jackson.core</groupId>
      <artifactId>jackson-databind</artifactId>
    </dependency>
    <dependency>
      <groupId>org.projectlombok</groupId>
      <artifactId>lombok</artifactId>
    </dependency>
  </dependencies>
</project>`

	path := writeTempFile(t, "pom.xml", pom)
	deps, err := parsePomXML(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 0)
}

func TestBuildGradleImplementation(t *testing.T) {
	gradle := `plugins {
    id 'org.springframework.boot' version '3.2.0'
    id 'java'
}

dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web:3.2.0'
    implementation 'org.springframework.boot:spring-boot-starter-data-redis:3.2.0'
    implementation 'org.postgresql:postgresql:42.6.0'
    runtimeOnly 'org.apache.kafka:kafka-clients:3.6.0'
    testImplementation 'org.springframework.boot:spring-boot-starter-test:3.2.0'
}
`
	path := writeTempFile(t, "build.gradle", gradle)
	deps, err := parseBuildGradle(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3: redis, postgresql, kafka
	// spring-boot-starter-web is unknown, testImplementation doesn't match regex
	assertDepCount(t, deps, 3)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "redis" && d.Port == 6379 && d.Confidence == model.Low
	}, "Redis from build.gradle")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "postgresql" && d.Port == 5432 && d.Confidence == model.Low
	}, "PostgreSQL from build.gradle")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "kafka" && d.Port == 9092 && d.Confidence == model.Low
	}, "Kafka from build.gradle")
}

func TestBuildGradleKts(t *testing.T) {
	gradle := `plugins {
    id("org.springframework.boot") version "3.2.0"
    kotlin("jvm") version "1.9.20"
}

dependencies {
    implementation("org.springframework.boot:spring-boot-starter-data-mongodb:3.2.0")
    implementation("com.rabbitmq:amqp-client:5.20.0")
    runtimeOnly("mysql:mysql-connector-java:8.0.33")
}
`
	path := writeTempFile(t, "build.gradle.kts", gradle)
	deps, err := parseBuildGradle(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 3)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "mongodb" && d.Port == 27017
	}, "MongoDB from build.gradle.kts")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "rabbitmq" && d.Port == 5672
	}, "RabbitMQ from build.gradle.kts")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "mysql" && d.Port == 3306
	}, "MySQL from build.gradle.kts")
}

func TestBuildGradleUnknownDepsIgnored(t *testing.T) {
	gradle := `dependencies {
    implementation 'org.springframework.boot:spring-boot-starter-web:3.2.0'
    implementation 'com.fasterxml.jackson.core:jackson-databind:2.15.0'
}
`
	path := writeTempFile(t, "build.gradle", gradle)
	deps, err := parseBuildGradle(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 0)
}

func TestBuildGradleCompileConfig(t *testing.T) {
	gradle := `dependencies {
    compile 'org.elasticsearch.client:elasticsearch-rest-high-level-client:7.17.0'
    compileOnly 'org.springframework.data:spring-data-elasticsearch:5.2.0'
}
`
	path := writeTempFile(t, "build.gradle", gradle)
	deps, err := parseBuildGradle(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertDepCount(t, deps, 2)

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "elasticsearch" && d.Port == 9200
	}, "Elasticsearch from compile")

	assertHasDep(t, deps, func(d model.NetworkDependency) bool {
		return d.Target == "elasticsearch" && d.Port == 9200
	}, "Elasticsearch from compileOnly")
}
