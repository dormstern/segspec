# Architecture Map

**Last updated:** v0.3.0 (2026-02-18)

## Module Structure

```
segspec/
  cmd/                   # CLI commands (cobra)
    root.go              # Root command, --format, --output flags
    analyze.go           # `segspec analyze <path|github-url>` — main pipeline
                         #   GitHub URL auto-clone to temp dir
                         #   Dual-mode AI routing (--ai flag)
                         #   Walker warnings display
    analyze_test.go      # E2E integration tests (5 tests)
    version.go           # `segspec version`
  internal/
    model/               # Core data types
      dependency.go      # NetworkDependency, DependencySet, Confidence
      dependency_test.go # 5 tests
    parser/              # Parser registry + individual parsers
      registry.go        # Register(), Match(), DefaultRegistry()
      registry_test.go   # 4 tests
      spring.go          # application.yml/properties — JDBC (with default ports),
                         #   Redis, Kafka, RabbitMQ, Spring Boot 3.x redis
                         #   (spring.data.redis), multi-document YAML (---)
      spring_test.go     # 14 tests
      compose.go         # docker-compose.yml — ports, depends_on, env, images
      compose_test.go    # 16 tests
      k8s.go             # K8s Deployment, Service, ConfigMap, multi-doc YAML
      k8s_test.go        # 6 tests
      envfile.go         # .env files — well-known vars, URL extraction
      envfile_test.go    # 12 tests
      buildfile.go       # pom.xml, build.gradle — library inference
      buildfile_test.go  # 7 tests
      testdata/fullstack/  # Integration test fixtures
    walker/              # File discovery and routing
      walker.go          # Walk() — recursive, skip dirs, dispatch to parsers
                         #   Returns (DependencySet, []WalkWarning, error)
      walker_test.go     # 5 tests
    renderer/            # Output formatters
      netpol.go          # K8s NetworkPolicy YAML generator
                         #   Default-deny base policies
                         #   Specific egress allow per dependency
                         #   Proper destination selectors:
                         #     podSelector, namespaceSelector, ipBlock
                         #   Port 0 skip, sanitizeName for label safety
      netpol_test.go     # 6 tests
      summary.go         # Human-readable summary with confidence warnings
      summary_test.go    # 4 tests
    ai/                  # AI-powered analysis (dual-mode)
      analyzer.go        # Analyze() — dual-mode AI dispatcher
                         #   analyzeLocal: Ollama + NuExtract (air-gapped)
                         #   analyzeCloud: Gemini Flash (cloud)
                         #   resolveProvider: auto-detect available backend
                         #   Secret redaction before cloud calls
                         #   30s HTTP timeout on all outbound requests
      analyzer_test.go   # 27 tests (mocked HTTP, no real API calls)
  main.go               # Entry point
```

## Key Abstractions

- `NetworkDependency`: Core data model. Source, Target, Port, Protocol, Description, Confidence, SourceFile.
- `DependencySet`: Collection with dedup by Key() and sorted output.
- `ParseFunc`: `func(path string) ([]NetworkDependency, error)` -- pure, stateless.
- `Registry`: Maps file patterns to ParseFuncs. Init()-registered.
- `WalkWarning`: Non-fatal warning from walker (unparseable files, permission errors).
- `ai.Analyze()`: Dual-mode AI -- local (Ollama/NuExtract) or cloud (Gemini Flash), auto-detected.

## Data Flow

```
segspec analyze <dir|github-url>
  → [if URL] clone to temp dir, validate URL, git timeout
  → walker.Walk(dir, registry)
    → filepath.WalkDir
    → registry.Match(filename) → []ParseFunc
    → each parser → []NetworkDependency
    → DependencySet.Add (dedup)
    → collect []WalkWarning (non-fatal issues)
  → [optional --ai] ai.Analyze(dir, existingDeps)
    → resolveProvider() (auto-detect Ollama vs Gemini)
    → collectFiles → redactSecrets → buildPrompt
    → analyzeLocal (NuExtract) or analyzeCloud (Gemini Flash)
    → parseResponse → DependencySet.Add (merge)
  → display walker warnings (if any)
  → renderer.Summary(ds) or renderer.NetworkPolicy(ds)
    → NetworkPolicy: default-deny + specific egress allows
    → proper selectors: podSelector/namespaceSelector/ipBlock
```

## Test Coverage

- **7 packages** all passing
- Unit tests per module + E2E integration tests
- AI module uses mocked HTTP client (no real API calls in tests)

## Integration Points

- **Ollama + NuExtract** (optional, via `--ai` flag): Local LLM for air-gapped environments
- **Gemini Flash** (optional, via `--ai` flag): Cloud LLM for higher accuracy, requires API key
- **GitHub** (optional): Auto-clone repos via HTTPS/git URLs with validation and timeout
- No other external dependencies at runtime

## Version History

| Version | Summary |
|---------|---------|
| v0.1.0 | Core parsers (Spring, Docker Compose, K8s, .env, pom.xml, build.gradle), NetworkPolicy renderer, GitHub URL auto-clone, GoReleaser + CI, README |
| v0.2.0 | Dual-mode AI (NuExtract local via Ollama + Gemini Flash cloud), --ai flag with auto-detect |
| v0.3.0 | 12 audit fixes: production-valid NetworkPolicies (proper selectors, default-deny), secret redaction, URL validation, JDBC default ports, Spring multi-doc YAML, Spring Boot 3.x redis, walker warnings, HTTP/git timeouts |
