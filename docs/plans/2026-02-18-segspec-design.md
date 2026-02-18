# segspec — Design Document

**Date:** 2026-02-18
**Status:** Approved
**Language:** Go

---

## What

A CLI tool that analyzes application config files and binaries to generate Kubernetes NetworkPolicy YAML. Instead of observing live traffic to discover what an app talks to, segspec analyzes the app itself.

## Why

Microsegmentation projects fail because nobody knows what their apps need to talk to. Security teams mandate segmentation, network teams can't implement without knowing traffic patterns, and app owners don't know their own dependencies. The result: 96% say microseg is important, 5% actually do it.

Current approaches require deploying agents or observing production traffic for weeks. segspec takes a different angle: the answer is already in the app's configs and code.

## Prior Art

- **AutoSeg** (Computers & Security, July 2025): Config-based analysis for K8s. 96.7% coverage across 184 services, ~2800 LoC Python, ~7 seconds. Config-only — no bytecode analysis.
- **ZTDJAVA** (ICSE 2025): Java Instrumentation API + ASM for least-privilege dependency policies. Supply chain focus, not network policies.
- **No existing tool** combines binary/code analysis with network policy generation.

## Target Users

Platform engineers, DevOps, and security teams operating Kubernetes clusters who need to generate or validate network segmentation policies.

## Phasing

### v0 — Config Parser (this phase)

**Input:** A directory containing application config files.

| File Type | What We Extract |
|-----------|----------------|
| `application.yml` / `.properties` | DB URLs, service endpoints, ports, Redis/Kafka/RabbitMQ connections |
| `docker-compose.yml` | Service names, ports, `depends_on`, networks, env vars |
| K8s manifests (`Deployment`, `Service`) | Container ports, service selectors, env vars |
| `.env` files | Connection strings, API URLs, service addresses |
| `pom.xml` / `build.gradle` | Dependencies (infer: uses Kafka? Redis? PostgreSQL?) |

**Output:**
1. Human-readable summary — "App `order-service` talks to: postgres:5432, redis:6379, user-service:8080"
2. Kubernetes NetworkPolicy YAML — ready to `kubectl apply -f`

**Architecture:**

```
CLI (cobra) → File walker → Parser registry → []NetworkDependency → Policy renderer → Output
```

Each parser is a standalone function registered by file pattern. No framework, no abstractions. Functions return `[]NetworkDependency`.

```go
type NetworkDependency struct {
    Source      string // app name / service name
    Target      string // hostname, service name, or IP
    Port        int
    Protocol    string // TCP, UDP
    Description string // "PostgreSQL database", "Redis cache", etc.
    Confidence  string // high, medium, low
    SourceFile  string // which config file this came from
}
```

**Tech stack:**
- Go 1.26
- cobra (CLI framework)
- go-yaml (YAML parsing)
- text/template (policy rendering)
- Zero external services, zero API keys

### v1 — Java Bytecode Analysis

Add JAR/WAR decompilation + bytecode scanning:
- Shell out to `cfr` decompiler
- Scan decompiled source for: Socket, HttpURLConnection, JDBC, Spring WebClient, @FeignClient, Kafka/RabbitMQ/Redis clients
- Merge with config-based findings
- Confidence scoring: config+bytecode match = high, bytecode-only = medium, config-only = medium

### v1.5 — Docker Image Analysis

Extract and scan Docker image layers:
- Pull/load image
- Extract filesystem
- Run v0+v1 parsers on extracted contents
- Parse Dockerfile for EXPOSE directives

### v2 — Multi-Format Output + Source Code Analysis

- Additional output formats: Terraform AWS Security Groups, CiliumNetworkPolicy, Calico GlobalNetworkPolicy
- Git repo analysis: clone, scan source files directly
- Richer dependency inference from import statements

### v3 — Runtime Validation

- javaagent-based instrumentation (inspired by ZTDJAVA)
- Run app, capture actual network calls
- Diff predicted vs. actual
- Confidence scoring per policy rule

## Design Decisions

1. **Go over Python** — Single binary distribution, K8s-native ecosystem fit, no runtime deps. Target users (DevOps/platform eng) expect Go CLI tools.
2. **K8s NetworkPolicy as primary output** — Universal standard supported by Calico and Cilium. Highest virality potential in the DevOps community. Other formats in v2.
3. **Confidence scoring from v0** — Every extracted dependency gets a confidence level. This directly addresses the #1 microseg blocker: enforcement fear.
4. **Parser registry pattern** — Each file type gets its own parser function. Easy to add new parsers without touching core logic.
5. **No external services** — Everything runs locally. No API keys, no accounts, no telemetry. Maximum trust for security-conscious users.
