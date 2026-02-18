# segspec Reddit Launch Posts

---

## Post 1: r/netsec

**Title:** CISA mandates microsegmentation. 95% of orgs don't enforce it. I built a tool that generates per-service K8s NetworkPolicies from config files in seconds.

**Body:**

I've been doing security engineering for a while and one thing drives me crazy: microsegmentation. Everyone agrees lateral movement is the #1 thing you need to contain. CISA's zero trust maturity model mandates it. Every compliance framework references it. Your CISO talks about it in board meetings.

And yet almost nobody actually does it.

The reason is simple -- the current workflow is brutal. You deploy your app wide open, install a traffic observation agent on every pod, wait 2-4 weeks to capture "normal" traffic patterns, then have a network engineer manually translate flow logs into NetworkPolicy YAML. Then you audit-mode it for another two weeks. Then something breaks at 2 AM and you roll back. Start over. The industry estimate is 4-8 weeks per application and $50K+ in engineering hours. Most teams give up after the observation period.

I kept thinking: the app already knows what it connects to. It's right there in the config files. `spring.datasource.url=jdbc:postgresql://db:5432/myapp`. `REDIS_HOST=redis-cache`. Service references in docker-compose.yml and k8s manifests. Why are we observing runtime traffic to discover what the source code already declares?

So I built **segspec** -- a CLI that reads application config files and outputs per-service Kubernetes NetworkPolicies with both ingress and egress rules. No agents, no runtime access, no observation period. It works completely offline from a single binary.

```bash
# Generate one NetworkPolicy per service with ingress + egress
segspec analyze ./your-app --format per-service

# Review each dependency before generating (interactive TUI)
segspec analyze ./your-app -i --format per-service
```

Each service gets its own policy defining exactly who can talk to it and what it can talk to. Default-deny for both directions. Proper podSelector, namespaceSelector, and ipBlock rules. DNS egress scoped to kube-system.

It parses Spring Boot configs, Docker Compose, K8s manifests, Helm charts (auto-renders via `helm template`), .env files, and build files. I tested it against Google's Online Boutique (11-service microservices demo) and it found 34 network dependencies in under a second. Zero false positives.

For config patterns that are harder to parse statically, there's an `--ai` flag with two modes:

- `--ai local` -- uses Ollama with the NuExtract model. Fully offline, nothing leaves your machine. Good for sensitive codebases and air-gapped environments.
- `--ai cloud` -- uses Gemini Flash on the free tier. Good for CI/CD pipelines.

The interactive mode (`-i`) lets you review every dependency, toggle what to include, and only then generate. AI-found dependencies are visually distinct so you know to verify them.

The idea isn't that this replaces flow-log validation in production. It's that you start with a real policy instead of starting wide open. Generate the baseline from config, apply it, then use your existing observability to catch anything you missed. Proactive instead of reactive.

Open source, MIT license, single Go binary: https://github.com/dormstern/segspec

Would love feedback from anyone who's actually been through the microseg pain. What patterns does your org use that I should support next?

---

## Post 2: r/kubernetes

**Title:** I got tired of writing NetworkPolicies by hand so I built a CLI that generates per-service ingress+egress policies from existing config files

**Body:**

Every Kubernetes cluster I've seen either has no NetworkPolicies at all or has stale ones that were written once and never updated. I get why -- writing them by hand is tedious, error-prone, and requires you to actually know every network dependency your app has. Which nobody does.

I built a tool called **segspec** that automates this. You point it at a directory (or a GitHub URL) and it reads all the config files it can find -- K8s manifests, Helm charts, Docker Compose, Spring Boot configs, .env files, pom.xml, build.gradle -- extracts every network dependency, and generates per-service NetworkPolicies with both ingress and egress rules.

```bash
segspec analyze ./my-app --format per-service
```

Each service gets its own policy. If `frontend` talks to `cartservice:8080`, then:
- `frontend` gets an egress rule allowing traffic to `cartservice:8080`
- `cartservice` gets an ingress rule allowing traffic from `frontend` on port 8080

Both get default-deny. DNS egress is scoped to kube-system. You can `kubectl apply` the output directly.

**Helm support:** segspec auto-detects `Chart.yaml` and runs `helm template` to render templates before parsing. Use `--helm-values values-prod.yaml` for environment-specific overrides.

**Interactive review:** Don't want to blindly apply 30+ rules? Use `--interactive` (or `-i`):

```
> [x] frontend -> cartservice:8080/TCP      [high]  K8s Service
  [x] frontend -> productcatalog:3550/TCP   [high]  K8s Service
  [ ] frontend -> adservice:9555/TCP        [med]   AI
  [x] cartservice -> redis-cart:6379/TCP    [high]  Spring

  up/down navigate  SPACE toggle  a all  n none  ENTER generate  q quit
```

Toggle individual dependencies, then generate from only what you selected. AI-discovered deps are visually distinct so you know what needs a second look.

A few other things worth noting:

- **No runtime agent.** Reads config files statically. No running cluster, no service mesh, no eBPF.
- **GitHub URL support.** `segspec analyze https://github.com/org/repo` clones + scans in one step.
- **Confidence levels.** Every dependency shows high/medium/low and the exact source file.
- **AI mode.** `--ai local` (Ollama, fully offline) or `--ai cloud` (Gemini Flash, free tier) for catching deps that live in code rather than config.

I tested it against Google's Online Boutique (11 services, 34 dependencies) and it mapped the entire service graph in under a second.

Open source, MIT licensed: https://github.com/dormstern/segspec

What would make this more useful for your setup? Any config formats or patterns I'm missing?

---

## Post 3: r/devops

**Title:** Your security team keeps asking for NetworkPolicies. Here's how to auto-generate per-service policies in CI.

**Body:**

If your org is anything like mine, the security team has been asking for Kubernetes NetworkPolicies for months. And nobody has time. Writing them by hand means mapping every network dependency for every service, keeping them updated as configs change, and coordinating with app teams who don't want to touch YAML they don't understand. So it just doesn't happen.

I built a tool called **segspec** that generates per-service NetworkPolicies from the config files already in your repo. It parses Spring Boot configs, Docker Compose, K8s manifests, Helm charts, .env files, and build files, extracts every network dependency, and outputs one NetworkPolicy per service with both ingress and egress rules.

Run it locally first to see what it finds:

```bash
# Summary of all discovered dependencies
segspec analyze ./your-app

# Interactive review -- toggle individual deps before generating
segspec analyze ./your-app --interactive

# Generate per-service policies
segspec analyze ./your-app --format per-service
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
          ./segspec analyze . --format per-service > k8s/network-policies.yaml

      - name: Commit if changed
        run: |
          git add k8s/network-policies.yaml
          git diff --cached --quiet || git commit -m "update network policies [segspec]"
          git push
```

Now every time someone merges a config change that adds a new database connection or a new service dependency, the NetworkPolicies update automatically. Each service gets its own policy with ingress and egress rules. Security team gets what they need, dev teams don't have to do anything extra.

**Helm charts work automatically** -- segspec detects `Chart.yaml` and renders templates before parsing. Use `--helm-values` for environment-specific overrides.

There's an `--ai` flag for catching dependencies that live in code rather than config files:

- `--ai local` -- runs Ollama with NuExtract on-box. Nothing leaves your machine. Good for air-gapped environments.
- `--ai cloud` -- uses Gemini Flash on the free tier (1,000 requests/day). Lightweight for CI, zero local setup.

For the GitHub Actions workflow above, cloud mode is the easy path -- just add `GEMINI_API_KEY` as a secret and append `--ai cloud` to the analyze command. For Jenkins behind a firewall, local mode with Ollama in a sidecar works well.

I tested this on Google's Online Boutique (11 microservices) and it found 34 dependencies in under a second. The generated YAML is valid `networking.k8s.io/v1` that you can `kubectl apply` directly.

Single Go binary, open source under MIT: https://github.com/dormstern/segspec

If you try it on your repos, I'd genuinely appreciate hearing what works and what doesn't. What would make this fit better into your pipeline?
