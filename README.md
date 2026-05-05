# segspec

**Catch unauthorized network changes in PR review, before they hit prod. Static analysis of app configs -- no agents, no runtime, no observation period.**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/dormstern/segspec/ci.yml?branch=main)](https://github.com/dormstern/segspec/actions)

---

A service quietly grows a new dependency. Memcached gets added, RabbitMQ gets removed, an env var points somewhere it shouldn't. None of it shows up cleanly in a 500-line YAML PR review. By the time it's in prod, the NetworkPolicy is already wrong.

segspec reads the configs you've already written -- Spring, Docker Compose, Kubernetes, Helm, .env, Maven/Gradle -- extracts every network dependency, and traces each one to the exact config line that declared it. Run it on a baseline JSON in CI with `--exit-code` and your pipeline fails the moment a service's network surface changes. Reviewers see intent, not noise.

## Gate PRs on Network Changes

![Diff demo](docs/demos/diff-demo.gif)

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

In CI, add `--exit-code` and the job fails on any change. The output names exactly what moved and the config line that caused it.

## Why This Exists

Every other tool in this lane wants to deploy an agent, watch your traffic for thirty days, and ship the inferred policy off to a control plane. That's a fine answer if you have the budget for a $40k+ runtime platform and the appetite for an observation period. Most teams don't.

segspec takes the opposite stance. It reads what you've already declared in source. It doesn't ask for an agent. It doesn't demand an observation window. It doesn't send your config off-box. It runs as a single static Go binary on a developer laptop or a CI runner, fully offline, and every dependency it reports is anchored to a specific `file:line` in your repo. If it's not in source, segspec doesn't claim it.

That makes it compatible with air-gapped clusters, regulated environments, and CI gates -- places runtime tooling can't go.

## What's Underneath: The Analyze Output

```bash
segspec analyze https://github.com/getsentry/self-hosted --format evidence
```

411 dependencies from Sentry's 70+ services in 11ms. Each one linked to the config line that created it.

![Evidence demo](docs/demos/evidence-demo.gif)

The diff above is just `analyze` run twice, structurally compared. The evidence trail is what makes the diff trustworthy.

## Output Formats

| Format | Flag | Use For |
|--------|------|---------|
| Summary | (default) | Eyeballing a service's deps |
| Evidence | `--format evidence` | Security review, audit trail |
| Audit ledger | `--format audit` | Auditor sign-off attached to a change ticket |
| Default-deny scaffold | `--format default-deny` | One-shot deny-by-default + explicit allow policies |
| JSON | `--format json` | Diff baseline, CMDB, scripting |
| NetworkPolicy | `--format netpol` | Single app policy |
| Per-service NetPol | `--format per-service` | One policy per service, ingress + egress (recommended) |

### Default-deny scaffold

`--format default-deny` emits a namespace-scoped default-deny `NetworkPolicy`
(`podSelector: {}` with empty `Ingress` and `Egress` rules) followed by every
per-service allow policy needed to override it for declared dependencies — in
one multi-document YAML, deterministically ordered (default-deny first, then
services alphabetical). This satisfies the auditor demand documented in
[cilium/cilium#43502](https://github.com/cilium/cilium/issues/43502) ("cluster
auditors want every component to have a targeted NetworkPolicy") in a single
command. If a namespace can be inferred from FQDNs in the input, the
default-deny policy carries it; otherwise the cluster admin fills it on apply.
Free tier.

```bash
segspec analyze ./services/orders --format default-deny > netpol.yaml
```

### Audit ledger

`--format audit` emits a Markdown document scoped to the way cluster
auditors actually review network changes. Each workload gets its own
section with separated **Ingress** and **Egress** tables, every row is
anchored to a `file:line` (secrets redacted), and confidence levels are
re-labelled as `approve` / `review` / `investigate` so a reviewer can
sweep the document top-to-bottom and tick boxes. A short SHA-256
fingerprint over the dependency keys lets two ledgers be compared at a
glance without diffing the whole file. The output ends with a sign-off
checklist (one NetworkPolicy per workload, low-confidence rows
confirmed, no rogue external egress) suitable for pasting into a
change-management ticket.

```bash
segspec analyze ./services/orders --format audit > orders-audit.md
```

JSON example:

```json
{
  "service": "your-app",
  "dependencies": [
    {
      "source": "your-app",
      "target": "postgres-primary",
      "port": 5432,
      "protocol": "TCP",
      "confidence": "high",
      "source_file": "src/main/resources/application.yml",
      "evidence_line": "spring.datasource.url: jdbc:postgresql://postgres-primary:5432/myapp"
    }
  ]
}
```

<details>
<summary>Example: per-service NetworkPolicy YAML</summary>

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
```

</details>

```bash
kubectl apply -f policies.yaml
```

Or commit to your GitOps repo and let ArgoCD/Flux handle it.

## Install

```bash
go install github.com/dormstern/segspec@latest
```

Or grab the binary:

```bash
curl -fsSL https://github.com/dormstern/segspec/releases/latest/download/segspec-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o /usr/local/bin/segspec
chmod +x /usr/local/bin/segspec
```

## Try it now

No repo to point it at? Two demo corpora ship inside the binary:

```bash
segspec analyze --demo sentry-mini
```

`--demo list` shows every available fixture. The corpora are
license-clean and fit in ~15 KB total — they exist so you can
see segspec's output on a realistic dependency graph without
cloning anything.

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

## CI Integration

### Gate PRs on network changes

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

## Supported Config Families

Spring Boot (`application.yml`/`.properties`), Docker Compose, Kubernetes (Deployments/Services/ConfigMaps), Helm charts (auto-rendered via `helm template`), `.env` files, and Maven/Gradle build files. Each parser extracts declared hosts, ports, protocols, and env-var references and links them back to source.

Helm is auto-detected. Pass `--helm-values values-prod.yaml` for custom values. If the `helm` CLI isn't available, segspec skips charts with a warning and continues.

## AI-Enhanced Analysis (Optional)

```bash
segspec analyze ./your-app --ai             # auto-detect (tries local first)
segspec analyze ./your-app --ai local       # fully offline via Ollama + NuExtract
segspec analyze ./your-app --ai cloud       # via Gemini Flash (free tier)
```

**Local** runs NuExtract (3.8B, MIT license) via Ollama. Nothing leaves your machine. **Cloud** uses Gemini Flash (1,000 free requests/day, set `GEMINI_API_KEY`). AI-discovered dependencies are marked medium confidence so you know to verify them.

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
