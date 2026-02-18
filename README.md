# segspec

🔥 **We replaced 6 weeks of security engineering with a 1-second CLI command.**

**From app configs to Kubernetes NetworkPolicies in seconds.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormorgenstern/segspec/ci.yml?branch=main)](https://github.com/dormstern/segspec/actions)

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

## Example: Google Online Boutique

- Dependencies discovered: 34
- Time: 0.8s (local machine)
- Policies: per service ingress + egress

## Roadmap / feedback

- More config formats
- Confidence scoring
- Policy enrichment plugins

---

⭐ **Star if you want microsegmentation without agents**
