# segspec Reddit Launch Posts

---

## Post 1: r/netsec

**Title:** CISA mandates microsegmentation. 95% of orgs don't enforce it. I built a tool that generates K8s NetworkPolicies from config files in seconds.

**Body:**

I've been doing security engineering for a while and one thing drives me crazy: microsegmentation. Everyone agrees lateral movement is the #1 thing you need to contain. CISA's zero trust maturity model mandates it. Every compliance framework references it. Your CISO talks about it in board meetings.

And yet almost nobody actually does it.

The reason is simple -- the current workflow is brutal. You deploy your app wide open, install a traffic observation agent on every pod, wait 2-4 weeks to capture "normal" traffic patterns, then have a network engineer manually translate flow logs into NetworkPolicy YAML. Then you audit-mode it for another two weeks. Then something breaks at 2 AM and you roll back. Start over. The industry estimate is 4-8 weeks per application and $50K+ in engineering hours. Most teams give up after the observation period.

I kept thinking: the app already knows what it connects to. It's right there in the config files. `spring.datasource.url=jdbc:postgresql://db:5432/myapp`. `REDIS_HOST=redis-cache`. Service references in docker-compose.yml and k8s manifests. Why are we observing runtime traffic to discover what the source code already declares?

So I built **segspec** -- a CLI that reads application config files and outputs valid Kubernetes NetworkPolicy YAML. No agents, no runtime access, no observation period. It works completely offline from a single binary.

Here's what it looks like:

```bash
segspec analyze ./your-app --format netpol
```

Output is production-valid NetworkPolicy YAML with proper podSelector, egress rules, ports, and protocols. DNS egress is always included. Every dependency shows a confidence level and the exact source file it was extracted from.

It parses Spring Boot configs (application.yml/properties), Docker Compose, K8s manifests, .env files, and build files (pom.xml, build.gradle). I tested it against Google's Online Boutique (11-service microservices demo) and it found 34 network dependencies in under a second. Zero false positives.

For config patterns that are harder to parse statically, there's an `--ai` flag with two modes:

- `--ai local` -- uses Ollama with the NuExtract model. Fully offline, nothing leaves your machine. Good for sensitive codebases and air-gapped environments.
- `--ai cloud` -- uses Gemini Flash on the free tier. Good for CI/CD pipelines.

AI-found dependencies get tagged with `[AI]` and medium confidence so you know to verify them.

The idea isn't that this replaces flow-log validation in production. It's that you start with a real policy instead of starting wide open. Generate the baseline from config, apply it, then use your existing observability to catch anything you missed. Proactive instead of reactive.

Open source, MIT license, single Go binary: https://github.com/dormstern/segspec

v0.3.0 just shipped with production-valid NetworkPolicy output.

Would love feedback from anyone who's actually been through the microseg pain. What patterns does your org use that I should support next? Anything in the output format that would make it more useful for your workflow?

---

## Post 2: r/kubernetes

**Title:** I got tired of writing NetworkPolicies by hand so I built a CLI that generates them from existing config files

**Body:**

Every Kubernetes cluster I've seen either has no NetworkPolicies at all or has stale ones that were written once and never updated. I get why -- writing them by hand is tedious, error-prone, and requires you to actually know every network dependency your app has. Which nobody does. You end up grepping through YAML, reading Spring configs, checking docker-compose files, tracing service references, and hoping you didn't miss anything.

I built a tool called **segspec** that automates this. You point it at a directory (or a GitHub URL) and it reads all the config files it can find -- K8s manifests, Docker Compose, Spring Boot configs, .env files, pom.xml, build.gradle -- extracts every network dependency, and generates a valid NetworkPolicy YAML.

```bash
segspec analyze ./my-app --format netpol
```

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: my-app-egress
  labels:
    generated-by: segspec
spec:
  podSelector:
    matchLabels:
      app: my-app
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
    - to: # DNS
      ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
```

A few things worth noting about how it works:

- **No runtime agent.** It reads config files statically. It doesn't need a running cluster, a service mesh, or eBPF. Just the source code.
- **No cluster access needed.** Runs entirely on your laptop as a single Go binary. Works offline.
- **GitHub URL support.** You can do `segspec analyze https://github.com/org/repo` and it clones + scans in one step.
- **Confidence levels.** Every dependency shows high/medium/low confidence and the exact source file where it was found. So you can review before applying.
- **DNS egress always included.** Because I've seen too many policies break DNS resolution.

I tested it against Google's Online Boutique (11 services, 34 dependencies) and it mapped the entire service graph in under a second. All high confidence, zero false positives.

There's also an `--ai` mode for catching dependencies that live in code rather than config. It runs locally via Ollama (fully offline) or via Gemini Flash on the free tier. AI-discovered deps are tagged so you know they need a second look.

The generated policies are v0.3.0 quality -- valid `networking.k8s.io/v1`, proper selectors, correct port/protocol pairs. You can `kubectl apply` them directly or drop them into your GitOps repo for ArgoCD/Flux to pick up.

Open source, MIT licensed: https://github.com/dormstern/segspec

I'm working on per-service policies with ingress rules next (right now it generates one aggregate egress policy). After that, Helm template resolution so you don't have to run `helm template` manually first.

What would make this more useful for your setup? Any config formats or patterns I'm missing? If you try it on your repos I'd love to hear what it catches and what it doesn't.

---

## Post 3: r/devops

**Title:** Your security team keeps asking for NetworkPolicies. Here's how to auto-generate them in CI.

**Body:**

If your org is anything like mine, the security team has been asking for Kubernetes NetworkPolicies for months. And nobody has time. Writing them by hand means mapping every network dependency for every service, keeping them updated as configs change, and coordinating with app teams who don't want to touch YAML they don't understand. So it just doesn't happen.

I built a tool called **segspec** that generates NetworkPolicies from the config files already in your repo. It parses Spring Boot configs, Docker Compose, K8s manifests, .env files, and build files, extracts every network dependency (databases, caches, brokers, service-to-service calls), and outputs valid NetworkPolicy YAML.

Run it locally first to see what it finds:

```bash
segspec analyze ./your-app --format netpol
```

Once you trust the output, drop it into your pipeline so policies stay in sync with code automatically:

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

      - name: Commit if changed
        run: |
          git add k8s/network-policy.yaml
          git diff --cached --quiet || git commit -m "update network policy [segspec]"
          git push
```

Now every time someone merges a config change that adds a new database connection or a new service dependency, the NetworkPolicy updates automatically. Security team gets what they need, dev teams don't have to do anything extra, and you're not manually maintaining YAML that's going to drift anyway.

There's an `--ai` flag for catching dependencies that live in code rather than config files. Two modes:

- `--ai local` -- runs Ollama with NuExtract on-box. Nothing leaves your machine. Good for air-gapped environments or if your security policy doesn't allow sending configs to external APIs.
- `--ai cloud` -- uses Gemini Flash on the free tier (1,000 requests/day). Lightweight for CI, zero local setup.

For the GitHub Actions workflow above, cloud mode is the easy path -- just add `GEMINI_API_KEY` as a secret and append `--ai cloud` to the analyze command. For Jenkins behind a firewall, local mode with Ollama in a sidecar works well.

I tested this on Google's Online Boutique (11 microservices) and it found 34 dependencies in under a second. All high confidence. The generated YAML is valid `networking.k8s.io/v1` that you can `kubectl apply` directly.

It's a single Go binary, open source under MIT: https://github.com/dormstern/segspec

v0.3.0 just shipped with production-valid NetworkPolicy output. Next up is per-service policies with ingress rules and a proper GitHub Action so you can replace the curl step with `uses: dormstern/segspec-action@v1`.

If you try it on your repos, I'd genuinely appreciate hearing what works and what doesn't. What config formats should I support next? What would make this fit better into your pipeline?
