# segspec

**From app configs to Kubernetes NetworkPolicies in seconds.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormorgenstern/segspec/ci.yml?branch=main)](https://github.com/dormstern/segspec/actions)

---

Microsegmentation is the #1 defense against lateral movement. CISA mandates it. Every CISO wants it. **Yet only 5% of organizations actually enforce it** -- because generating network policies takes 4-8 weeks per app, costs $50K+ in engineering hours, and requires three teams to coordinate simultaneously.

segspec reads your application config files and generates ready-to-apply Kubernetes NetworkPolicy YAML. No agents. No runtime access. No observation period.

## What It Does

Point segspec at any application repository. It extracts every network dependency -- databases, caches, message brokers, APIs, service-to-service calls -- from config files and outputs per-service NetworkPolicies with both ingress and egress rules.

**Supported config types:**

| Type | Files |
|------|-------|
| Spring Boot | `application.yml`, `application.properties` |
| Docker Compose | `docker-compose.yml` |
| Kubernetes | Deployments, Services, ConfigMaps |
| Helm Charts | `Chart.yaml` + templates (auto-detected, rendered via `helm template`) |
| Environment | `.env` files |
| Build | `pom.xml`, `build.gradle` |

## Quick Start

**Install:**

```bash
go install github.com/dormstern/segspec@latest
```

Or download the binary:

```bash
curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o /usr/local/bin/segspec
chmod +x /usr/local/bin/segspec
```

**Run:**

```bash
# Scan a local directory
segspec analyze ./your-app

# Or point at a GitHub URL directly
segspec analyze https://github.com/org/repo

# Generate per-service policies with ingress + egress
segspec analyze ./your-app --format per-service

# Review dependencies interactively before generating
segspec analyze ./your-app --interactive

# Combine everything: AI + interactive review + per-service output
segspec analyze ./your-app --ai -i --format per-service
```

No cluster access, no cloud credentials, no running application. It works offline.

## Real-World Example

We ran segspec against [Google's Online Boutique](https://github.com/GoogleCloudPlatform/microservices-demo) -- an 11-service microservices reference architecture.

**Result: 34 network dependencies found in under 1 second.**

segspec mapped the entire service mesh:

- `frontend` -> cartservice, checkoutservice, currencyservice, productcatalogservice, recommendationservice, adservice, shippingservice, shoppingassistantservice
- `checkoutservice` -> cartservice, currencyservice, emailservice, paymentservice, productcatalogservice, shippingservice
- `cartservice` -> redis-cart:6379
- `loadgenerator` -> frontend:80
- Plus all container ports, service ports, and targetPort mappings

All at high confidence. Zero false positives.

## Output Formats

### Summary (default)

```bash
segspec analyze ./your-app
```

```
Service: your-app
Dependencies: 12

  -> postgres-primary:5432/TCP  [high]  PostgreSQL
     source: your-app/src/main/resources/application.yml
  -> redis-cache:6379/TCP       [high]  Redis
     source: your-app/docker-compose.yml
  -> kafka-broker:9092/TCP      [high]  Kafka
     source: your-app/src/main/resources/application.yml

Confidence: 10 high, 1 medium, 1 low
```

### NetworkPolicy YAML (single app)

```bash
segspec analyze ./your-app --format netpol > network-policy.yaml
```

Generates a default-deny policy + egress allow rules for one application.

### Per-Service Policies (recommended for multi-service repos)

```bash
segspec analyze ./your-app --format per-service > policies.yaml
```

Generates one NetworkPolicy **per service** with both ingress and egress rules. This is what you want for real microsegmentation -- every service gets its own policy defining exactly who can talk to it and what it can talk to.

<details>
<summary>Example: per-service output for a frontend + cartservice + redis setup</summary>

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

Apply it:

```bash
kubectl apply -f policies.yaml
```

Or commit to your GitOps repo and let ArgoCD/Flux handle it.

## Interactive Mode

Don't trust the output blindly? Use `--interactive` (or `-i`) to review every dependency before generating YAML:

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

Navigate with arrow keys or j/k, toggle with space, press Enter to generate from only the selected dependencies. Low-confidence AI discoveries are visually distinct so you know what to double-check.

## Helm Chart Support

segspec auto-detects Helm charts (directories with `Chart.yaml`) and renders them via `helm template` before parsing. No extra flags needed -- just point it at a repo containing Helm charts.

```bash
# Auto-detects and renders Helm charts
segspec analyze ./my-helm-app

# Use a specific values file (e.g. production overrides)
segspec analyze ./my-helm-app --helm-values values-prod.yaml
```

Requires `helm` to be installed. If helm isn't available, segspec skips chart rendering with a warning and continues analyzing other config files normally.

## AI-Enhanced Analysis

Add `--ai` to find dependencies the rule-based parsers might miss. Pick your mode:

### Option A: Local (fully offline, nothing leaves your machine)

```bash
# One-time setup: install Ollama + the NuExtract extraction model
curl -fsSL https://ollama.com/install.sh | sh
ollama pull nuextract

# Run with local AI
segspec analyze ./your-app --ai local
```

**Best for:** enterprise, air-gapped networks, sensitive codebases. NuExtract is a 3.8B model by NuMind (US/France) under MIT license, built on Microsoft's Phi-3. No data leaves your laptop.

### Option B: Cloud (zero install, free tier)

```bash
# One-time: get a free API key at https://aistudio.google.com/apikey
export GEMINI_API_KEY=your-key-here

# Run with cloud AI
segspec analyze ./your-app --ai cloud
```

**Best for:** quick evaluation, CI/CD pipelines, teams that don't want to manage local models. Uses Google's Gemini Flash (free tier: 1,000 requests/day).

### Auto-detect

```bash
segspec analyze ./your-app --ai
```

Without `local` or `cloud`, segspec checks for Ollama first (privacy-first default), then falls back to Gemini if `GEMINI_API_KEY` is set.

AI-discovered dependencies are marked with `[AI]` prefix and medium confidence so you know to verify them.

## CI/CD Integration

Add segspec to your pipeline to keep network policies in sync with code:

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

## CLI Reference

```
segspec analyze <path>  [flags]

Arguments:
  <path>    Local directory or GitHub URL (https://github.com/org/repo)

Flags:
  -f, --format string       Output format: summary, netpol, per-service, all (default "summary")
  -o, --output string       Write output to file (default: stdout)
  -i, --interactive         Review dependencies interactively before generating
      --ai [string]         AI backend: local (Ollama), cloud (Gemini), or omit for auto-detect
      --helm-values string  Helm values file for rendering charts
```

## Roadmap

- First-party GitHub Action -- `uses: dormstern/segspec-action@v1`
- Multi-format output -- AWS Security Groups, Cilium, Calico

## Contributing

Contributions welcome. Open an issue first for anything non-trivial. PRs should include tests.

## License

[MIT](LICENSE)
