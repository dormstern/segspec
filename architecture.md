# Architecture Map

**Last updated:** v1.0.0 complete (2026-02-18)

## Module Structure

```
segspec/
  cmd/                   # CLI commands (cobra)
    root.go              # Root command, --format, --output flags
    analyze.go           # `segspec analyze <path>` — main pipeline
    analyze_test.go      # E2E integration tests (5 tests)
    version.go           # `segspec version`
  internal/
    model/               # Core data types
      dependency.go      # NetworkDependency, DependencySet, Confidence
      dependency_test.go # 5 tests
    parser/              # Parser registry + individual parsers
      registry.go        # Register(), Match(), DefaultRegistry()
      registry_test.go   # 4 tests
      spring.go          # application.yml/properties — JDBC, Redis, Kafka, RabbitMQ
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
      walker_test.go     # 5 tests
    renderer/            # Output formatters
      netpol.go          # K8s NetworkPolicy YAML generator
      netpol_test.go     # 6 tests
      summary.go         # Human-readable summary with confidence warnings
      summary_test.go    # 4 tests
    ai/                  # AI-powered analysis
      analyzer.go        # Analyze() — file collection, Claude API, response parsing
      analyzer_test.go   # 27 tests (mocked HTTP, no real API calls)
  main.go               # Entry point
```

## Key Abstractions

- `NetworkDependency`: Core data model. Source, Target, Port, Protocol, Description, Confidence, SourceFile.
- `DependencySet`: Collection with dedup by Key() and sorted output.
- `ParseFunc`: `func(path string) ([]NetworkDependency, error)` — pure, stateless.
- `Registry`: Maps file patterns to ParseFuncs. Init()-registered.
- `ai.Analyze()`: Collects configs, calls Claude API, merges AI findings.

## Data Flow

```
segspec analyze <dir>
  → walker.Walk(dir, registry)
    → filepath.WalkDir
    → registry.Match(filename) → []ParseFunc
    → each parser → []NetworkDependency
    → DependencySet.Add (dedup)
  → [optional] ai.Analyze(dir, existingDeps)
    → collectFiles → buildPrompt → callAPI → parseResponse
    → DependencySet.Add (merge)
  → renderer.Summary(ds) or renderer.NetworkPolicy(ds)
```

## Test Coverage

- **139 tests** across 7 packages, all passing
- Unit tests per module + E2E integration tests
- AI module uses mocked HTTP client (no real API calls in tests)

## Integration Points

- **Anthropic API** (optional, via `--ai` flag): POST to /v1/messages with Claude Sonnet
- No other external dependencies at runtime
