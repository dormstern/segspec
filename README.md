# segspec

**From app configs to Kubernetes NetworkPolicies in seconds.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormorgenstern/segspec/ci.yml?branch=main)](https://github.com/dormorgenstern/segspec/actions)

---

Microsegmentation is the #1 defense against lateral movement. CISA mandates it. Every CISO wants it. **Yet only 5% of organizations actually enforce it** -- because generating network policies takes 4-8 weeks per app, costs $50K+ in engineering hours, and requires three teams to coordinate simultaneously.

segspec reads your application config files and generates ready-to-apply Kubernetes NetworkPolicy YAML. No agents. No runtime access. No observation period.

## What It Does

Point segspec at any application repository. It extracts every network dependency -- databases, caches, message brokers, APIs, service-to-service calls -- from config files and outputs a valid NetworkPolicy.

**Supported config types:**

| Type | Files |
|------|-------|
| Spring Boot | `application.yml`, `application.properties` |
| Docker Compose | `docker-compose.yml` |
| Kubernetes | Deployments, Services, ConfigMaps |
| Environment | `.env` files |
| Build | `pom.xml`, `build.gradle` |

## Quick Start

**Install:**

```bash
go install github.com/dormorgenstern/segspec@latest
```

Or download the binary:

```bash
curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o /usr/local/bin/segspec
chmod +x /usr/local/bin/segspec
```

**Run:**

```bash
segspec analyze ./your-app
```

Or point it at a GitHub URL directly:

```bash
segspec analyze https://github.com/org/repo
```

That's it. No cluster access, no cloud credentials, no running application. It works offline.

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

### NetworkPolicy YAML

```bash
segspec analyze ./your-app --format netpol > network-policy.yaml
```

<details>
<summary>Example output</summary>

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: your-app-egress
  labels:
    generated-by: segspec
spec:
  podSelector:
    matchLabels:
      app: your-app
  policyTypes:
    - Egress
  egress:
    - to: # postgres-primary
      ports:
        - port: 5432
          protocol: TCP
    - to: # redis-cache
      ports:
        - port: 6379
          protocol: TCP
    - to: # kafka-broker
      ports:
        - port: 9092
          protocol: TCP
    - to: # DNS (always included)
      ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
```

</details>

Apply it:

```bash
kubectl apply -f network-policy.yaml
```

Or commit to your GitOps repo and let ArgoCD/Flux handle it.

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
          ./segspec analyze . --format netpol > k8s/network-policy.yaml

      - name: Commit policy
        run: |
          git add k8s/network-policy.yaml
          git diff --cached --quiet || git commit -m "update network policy [segspec]"
```

## Roadmap

- Per-service policies -- one NetworkPolicy per service with ingress rules
- Helm template resolution -- `helm template` + analysis in one step
- First-party GitHub Action -- `uses: dormstern/segspec-action@v1`
- Multi-format output -- AWS Security Groups, Cilium, Calico
- Interactive mode -- review and edit dependencies before generating YAML

## Contributing

Contributions welcome. Open an issue first for anything non-trivial. PRs should include tests.

## License

[MIT](LICENSE)
