# Progress Log

## Architecture Decisions

- **Go 1.26**: Single binary distribution for K8s-native audience. Chosen over Python (runtime dep friction) and TypeScript (ecosystem mismatch). See docs/plans/2026-02-18-segspec-design.md.
- **Cobra CLI**: Industry standard for Go CLI tools (kubectl, helm, cilium all use it).
- **Parser registry pattern**: Each file type gets its own parser function. Pure functions: file path in, []NetworkDependency out. No shared state.
- **K8s NetworkPolicy primary output**: Highest virality in DevOps community. Universal standard across Calico/Cilium. Other formats deferred to v2.
- **Confidence scoring from v0**: Every extracted dependency gets high/medium/low confidence. Addresses the #1 microseg blocker: enforcement fear.

## Open Questions

- OQ-001: Should we support scanning remote Git repos in v1, or local directories only? → RESOLVED in v0.1.0: GitHub URL auto-clone implemented.
- OQ-002: How to handle conflicting dependency declarations (e.g., config says port 5432, env var says 5433)? → Flag both with medium confidence.
- OQ-003: Should the tool infer service names from directory structure or require explicit naming? → Start with directory name, allow --name override.

## Cross-Cutting Patterns

- **Parallel audit agents work well**: Running QA, Security, and Architecture audit agents in parallel, then running parallel fix agents on their findings, is an effective pattern for quality passes. Each agent has a focused lens and produces actionable findings.
- **NetworkPolicy destination selectors are critical**: Empty selectors in NetworkPolicies are syntactically valid but semantically dangerous -- they match everything. Production-valid policies require explicit podSelector, namespaceSelector, or ipBlock for each egress rule.
- **Secret redaction must happen before any cloud AI call**: This is a hard security invariant. Redaction must be applied to all file contents before they leave the machine, regardless of the AI provider.
- **Default ports for JDBC are essential**: Many real-world Spring configs omit the port from JDBC URLs (e.g., `jdbc:postgresql://db-host/mydb`). The parser must infer default ports (PostgreSQL 5432, MySQL 3306, etc.) or dependencies get port 0, which is useless for NetworkPolicies.
- **Spring multi-document YAML (profiles) is common**: Real-world Spring apps routinely use `---` separators for profile-based configs. Parsing only the first document misses production/staging/dev overrides.

## Active Gotchas

- **Multiple agents modifying same files needs integration verification**: When parallel fix agents each modify different parts of the codebase, an integration verification step is needed afterward to ensure changes compose correctly. Tests must be run after all fixes are merged, not just after each individual fix.
- **Spring Boot 3.x broke redis config key**: `spring.redis.*` moved to `spring.data.redis.*` in Spring Boot 3.x. Both must be supported since projects migrate at different speeds.

## Drift Reports

## Contract Changes

---

## Recent Cycle Entries

### v0.3.0 Audit Cycle (2026-02-18)

**Pattern discovered**: Parallel audit agents (QA, Security, Architecture) followed by parallel fix agents is an effective quality cycle. Each agent has a focused lens -- QA finds correctness issues, Security finds data handling issues, Architecture finds structural issues. Running them in parallel saves time. Then fix agents can also run in parallel on independent issues.

**Gotcha**: When multiple fix agents modify overlapping files (e.g., both netpol.go changes and analyzer.go changes touch the analyze.go command wiring), integration verification is mandatory after all fixes land. A fix that passes in isolation may break when composed with another fix.

**Key learnings from 12 audit fixes**:
1. NetworkPolicy selectors: empty `to:` blocks match all destinations -- this is the opposite of zero-trust. Every egress rule needs an explicit podSelector, namespaceSelector, or ipBlock.
2. Default-deny must be the base policy. Specific allows are layered on top.
3. Secret redaction is a non-negotiable invariant before any cloud API call.
4. JDBC default ports (5432, 3306, etc.) must be inferred when omitted -- this is the common case in Spring configs.
5. Spring multi-document YAML (`---` separators for profiles) is ubiquitous in production apps.
6. Spring Boot 3.x moved `spring.redis` to `spring.data.redis` -- both paths must be checked.
7. Walker should return warnings (not errors) for unparseable files so the pipeline continues.
8. HTTP and git clone operations need explicit timeouts (30s) to prevent indefinite hangs.

---

## Release: v0.1.0

Core parsers (Spring, Docker Compose, K8s, .env, pom.xml, build.gradle), GitHub URL auto-clone, NetworkPolicy renderer, human-readable summary, GoReleaser + CI, README.

## Release: v0.2.0

Dual-mode AI analysis: NuExtract local via Ollama for air-gapped environments, Gemini Flash cloud for higher accuracy. --ai flag with auto-detect of available backend.

## Release: v0.3.0

12 audit fixes for production readiness: production-valid NetworkPolicies with proper destination selectors and default-deny, secret redaction before cloud AI calls, URL validation, JDBC default port inference, Spring multi-document YAML, Spring Boot 3.x redis config key, walker warnings return type, HTTP/git timeouts.
