# Architecture Map

**Last updated:** v1.0.0 bootstrap (2026-02-18)

## Module Structure

```
segspec/
  cmd/             # CLI commands (cobra)
    root.go        # Root command, global flags
    analyze.go     # `segspec analyze <path>` command
    version.go     # `segspec version` command
  internal/
    model/         # NetworkDependency struct, collection types
    parser/        # Parser registry + individual parsers
      registry.go  # Parser registration and dispatch
      spring.go    # application.yml / .properties
      compose.go   # docker-compose.yml
      k8s.go       # K8s Deployment, Service, ConfigMap
      envfile.go   # .env files
      buildfile.go # pom.xml, build.gradle (dependency inference)
    walker/        # File discovery and routing
    renderer/      # NetworkDependency → output formats
      netpol.go    # K8s NetworkPolicy YAML
      summary.go   # Human-readable summary
  main.go          # Entry point
```

## Key Abstractions

- `NetworkDependency`: Core data model. Source app, target host/service, port, protocol, confidence, source file.
- `Parser`: Function signature `func(path string) ([]NetworkDependency, error)`. Pure, stateless.
- `ParserRegistry`: Maps file glob patterns to parser functions. Walker uses this to route files.

## Integration Points

- None (fully offline CLI tool)

## Hot Paths

- Not yet established (pre-build)

## Test Coverage

- Not yet established (pre-build)
