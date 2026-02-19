# segspec

🔥 **We replaced 6 weeks of security engineering with a 1-second CLI command.**

**From app configs to Kubernetes NetworkPolicies in seconds.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormstern/segspec/ci.yml?branch=main)](https://github.com/dormstern/segspec/actions)

---

Microsegmentation is the #1 defense against lateral movement. CISA mandates it. Every CISO wants it.  
**Yet only ~5% of organizations actually enforce it**—because generating network policies takes weeks, costs $50K+ in engineering hours, and requires three teams to coordinate simultaneously.

`segspec` reads your application config files and generates ready-to-apply Kubernetes `NetworkPolicy` YAML.  
No agents. No runtime access. No observation window.

## What it does

Point segspec at any application repository. It extracts network dependencies—databases, caches, message brokers, APIs, and service-to-service calls—from configs and outputs per-service NetworkPolicies with ingress and egress rules.

### Supported config types

| Type | Files |
|---|---|
| Spring Boot | `application.yml`, `application.properties` |
| Docker Compose | `docker-compose.yml` |
| Kubernetes | Deployments, Services, ConfigMaps |
| Helm charts | `Chart.yaml` + templates (rendered via `helm template`) |

## Why this shouldn't exist

Microsegmentation vendors and runtime inspection tools say you need agents and a 30–60 day “learning period”.  
But most dependencies are already declared in your **source-of-truth configs**, so why are we paying for packet inspection?

## Quick start

```bash
go install github.com/dormstern/segspec/cmd/segspec@latest
```

### Analyze a repo

```bash
segspec analyze https://github.com/GoogleCloudPlatform/microservices-demo --format per-service --output ./networkpolicies
```

### Apply (optional)

```bash
kubectl apply -f ./networkpolicies
```

## Real-world results

We scanned real production apps -- not demos -- to show what segspec finds in software thousands of companies actually run.

### Sentry self-hosted (70+ services)

[getsentry/self-hosted](https://github.com/getsentry/self-hosted) -- the most popular open-source error monitoring platform.

![Sentry scan](docs/demos/sentry-scan.gif)

| Metric | Result |
|--------|--------|
| Dependencies found | 411 |
| NetworkPolicies generated | 71 |
| Scan time | 11ms |
| Infrastructure | Kafka, Redis, Memcached, PostgreSQL, ClickHouse, Snuba, Symbolicator |

### PostHog (25+ services)

[PostHog/posthog](https://github.com/PostHog/posthog) -- open-source product analytics used by 100K+ teams.

![PostHog scan](docs/demos/posthog-scan.gif)

| Metric | Result |
|--------|--------|
| Dependencies found | 23 |
| NetworkPolicies generated | 12 |
| Scan time | 128ms |
| Infrastructure | Kafka, Redis, PostgreSQL, Redpanda, MinIO |

### Google Online Boutique (11 services)

[GoogleCloudPlatform/microservices-demo](https://github.com/GoogleCloudPlatform/microservices-demo) -- Google's microservices reference architecture.

- 34 dependencies found in under 1 second
- Entire service mesh mapped at high confidence, zero false positives

## Roadmap

- GitHub Action -- `uses: dormstern/segspec-action@v1`
- Multi-format output -- AWS Security Groups, Cilium, Calico

---

⭐ **Star if you want security-as-config**
