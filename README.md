# segspec

**Extract network dependencies from app configs. Generate Kubernetes NetworkPolicies. Diff changes in CI.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormorgenstern/segspec/ci.yml?branch=main)](https://github.com/dormstern/segspec/actions)

---

segspec reads your application config files -- Spring, Docker Compose, Kubernetes, Helm, .env, Maven/Gradle -- and maps every network dependency. Each one is traced to the exact config line that declared it. Output as a summary, evidence report, JSON, or ready-to-apply NetworkPolicy YAML.

```bash
segspec analyze https://github.com/getsentry/self-hosted --format evidence
```

411 dependencies from Sentry's 70+ services in 11ms. Each one linked to the config line that created it.

## See It In Action

![Evidence demo](docs/demos/evidence-demo.gif)

No agents, no runtime access, no observation period. Works offline.

## Install

```bash
go install github.com/dormstern/segspec@latest
```

Or grab the binary:

```bash
curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o /usr/local/bin/segspec
chmod +x /usr/local/bin/segspec
```

## Usage

```bash
# Scan a local directory or GitHub URL
segspec analyze ./your-app
segspec analyze https://github.com/org/repo

# Show evidence -- the exact config line behind each dependency
segspec analyze ./your-app --format evidence

# Generate per-service NetworkPolicies (ingress + egress)
segspec analyze ./your-app --format per-service

# JSON output for scripting or as a diff baseline
segspec analyze ./your-app --format json -o baseline.json

# Diff against a baseline -- use --exit-code in CI to block on changes
segspec diff baseline.json ./your-app --exit-code
```

## What Happens When Something Changes

![Diff demo](docs/demos/diff-demo.gif)

Someone on your team adds a memcached dependency and removes RabbitMQ. You'd never know -- unless you're diffing:

```bash
segspec diff baseline.json ./your-app
```

```
Network Dependency Changes
==========================

ADDED (2):
  + your-app → memcached:11211/TCP [medium]
    Evidence: MEMCACHED_HOST=memcached:11211
  + memcached → memcached:11211/TCP [high]
    Evidence: ports: 11211:11211

REMOVED (1):
  - your-app → rabbitmq:5672/TCP [high]
    Was in: src/main/resources/application.yml

UNCHANGED: 13 dependencies
```

Shows what changed and the config line that caused it -- so reviewers see intent, not a 500-line YAML diff.

## Output Formats

### Summary (default)

```bash
segspec analyze ./your-app
```

```
Service: your-app
Dependencies: 14

  → postgres-primary:5432/TCP  [high]  PostgreSQL
    source: src/main/resources/application.yml
  → redis-cache:6379/TCP       [high]  Redis
    source: docker-compose.yml
  → kafka-broker:9092/TCP      [high]  Kafka
    source: src/main/resources/application.yml

Confidence: 10 high, 4 medium, 0 low
```

### Evidence Report

```bash
segspec analyze ./your-app --format evidence
```

Every dependency with the exact config line that declared it. Useful for security reviews and compliance audits.

### JSON

```bash
segspec analyze ./your-app --format json -o deps.json
```

```json
{
  "service": "your-app",
  "generated": "2026-02-23",
  "version": "0.5.0",
  "dependencies": [
    {
      "source": "your-app",
      "target": "postgres-primary",
      "port": 5432,
      "protocol": "TCP",
      "description": "PostgreSQL",
      "confidence": "high",
      "source_file": "src/main/resources/application.yml",
      "evidence_line": "spring.datasource.url: jdbc:postgresql://postgres-primary:5432/myapp"
    }
  ]
}
```

Machine-readable. Use as a diff baseline, pipe into your own tooling, or feed to your CMDB.

### NetworkPolicy YAML

```bash
segspec analyze ./your-app --format netpol > policy.yaml         # single app
segspec analyze ./your-app --format per-service > policies.yaml   # per service (recommended)
```

Per-service generates one NetworkPolicy per service with both ingress and egress rules. Each service gets its own policy.

<details>
<summary>Example: per-service output for frontend + cartservice + redis</summary>

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: frontend-netpol
  labels:
    generated-by: segspec
spec:
  podSelector:
    matchLabels:
      app: frontend
  policyTypes:
    - Ingress
    - Egress
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: cartservice
      ports:
        - port: 8080
          protocol: TCP
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: cartservice-netpol
  labels:
    generated-by: segspec
spec:
  podSelector:
    matchLabels:
      app: cartservice
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: frontend
      ports:
        - port: 8080
          protocol: TCP
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: redis
      ports:
        - port: 6379
          protocol: TCP
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
```

</details>

```bash
kubectl apply -f policies.yaml
```

Or commit to your GitOps repo and let ArgoCD/Flux handle it.

## Put It In Your Pipeline

### Gate PRs on network changes

Fail the check if network dependencies changed since the last baseline:

```yaml
# .github/workflows/netpol-diff.yml
name: Network Policy Drift
on: [pull_request]

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Check for network dependency changes
        run: |
          curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-linux-amd64 -o segspec
          chmod +x segspec
          ./segspec diff deps-baseline.json . --exit-code
```

Exit code 1 means something changed. The diff output shows exactly what and the config line that caused it.

### Auto-generate policies on merge

```yaml
# .github/workflows/netpol.yml
name: Network Policies
on:
  push:
    branches: [main]

jobs:
  generate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Generate Network Policies
        run: |
          curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-linux-amd64 -o segspec
          chmod +x segspec
          ./segspec analyze . --format per-service > k8s/network-policies.yaml
      - name: Commit policy
        run: |
          git add k8s/network-policies.yaml
          git diff --cached --quiet || git commit -m "update network policies [segspec]"
```

## Interactive Mode

Review every dependency before committing to a policy:

```bash
segspec analyze ./your-app --interactive --format per-service
```

```
 segspec: 34 dependencies found (34 selected)

> [x] frontend -> cartservice:8080/TCP      [high]  K8s Service
  [x] frontend -> productcatalog:3550/TCP   [high]  K8s Service
  [ ] frontend -> adservice:9555/TCP        [med]   AI
  [x] cartservice -> redis-cart:6379/TCP    [high]  Spring
  ...

  up/down navigate  SPACE toggle  a all  n none  ENTER generate  q quit
```

Toggle with space, press Enter to generate from only what you've approved.

## What It Reads

| Type | Files | What It Extracts |
|------|-------|------------------|
| Spring Boot | `application.yml`, `application.properties` | JDBC URLs, Redis/Kafka/RabbitMQ hosts, custom endpoints |
| Docker Compose | `docker-compose.yml` | depends_on, linked services, exposed ports, env vars |
| Kubernetes | Deployments, Services, ConfigMaps | Container ports, service ports, env var references |
| Helm Charts | `Chart.yaml` + templates | Auto-rendered via `helm template`, then parsed as K8s |
| Environment | `.env` files | Database URLs, service hosts, API endpoints |
| Build | `pom.xml`, `build.gradle` | Infrastructure dependencies (kafka, redis, etc.) |

## AI-Enhanced Analysis

Add `--ai` to find dependencies the rule-based parsers might miss:

```bash
segspec analyze ./your-app --ai             # auto-detect (tries local first)
segspec analyze ./your-app --ai local       # fully offline via Ollama + NuExtract
segspec analyze ./your-app --ai cloud       # via Gemini Flash (free tier)
```

**Local** runs NuExtract (3.8B, MIT license) via Ollama. Nothing leaves your machine. **Cloud** uses Gemini Flash (1,000 free requests/day, set `GEMINI_API_KEY`).

AI-discovered dependencies are marked with medium confidence so you know to verify them.

## Helm Charts

Auto-detected. No flags needed:

```bash
segspec analyze ./my-helm-app
segspec analyze ./my-helm-app --helm-values values-prod.yaml   # custom values
```

Requires `helm` CLI. If unavailable, segspec skips charts with a warning and continues.

## CLI Reference

```
segspec analyze <path> [flags]

  <path>    Local directory or GitHub URL

  -f, --format string       summary, netpol, per-service, all, evidence, json (default "summary")
  -o, --output string       Write output to file
  -i, --interactive         Review dependencies before generating
      --ai [string]         AI: local (Ollama), cloud (Gemini), or auto-detect
      --helm-values string  Helm values file
```

```
segspec diff <baseline.json> <path> [flags]

  <baseline.json>   JSON from `segspec analyze --format json`
  <path>            Directory or GitHub URL to compare against

      --exit-code   Exit 1 if changes detected (for CI)
```

## Roadmap

- GitHub Action -- `uses: dormstern/segspec-action@v1`
- AWS Security Groups, Cilium, Calico output formats
- Terraform security group generation

## Author

Built by [Dor Morgenstern](https://www.linkedin.com/in/dor-morgenstern/) — ex-product lead at Orchid Security and Torq, building and shipping network and security products for over a decade.

## Contributing

Contributions welcome. Open an issue first for anything non-trivial. PRs should include tests.

## License

[MIT](LICENSE)
