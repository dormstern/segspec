# Progress Log

## Architecture Decisions

- **Go 1.26**: Single binary distribution for K8s-native audience. Chosen over Python (runtime dep friction) and TypeScript (ecosystem mismatch). See docs/plans/2026-02-18-segspec-design.md.
- **Cobra CLI**: Industry standard for Go CLI tools (kubectl, helm, cilium all use it).
- **Parser registry pattern**: Each file type gets its own parser function. Pure functions: file path in, []NetworkDependency out. No shared state.
- **K8s NetworkPolicy primary output**: Highest virality in DevOps community. Universal standard across Calico/Cilium. Other formats deferred to v2.
- **Confidence scoring from v0**: Every extracted dependency gets high/medium/low confidence. Addresses the #1 microseg blocker: enforcement fear.

## Open Questions

- OQ-001: Should we support scanning remote Git repos in v1, or local directories only? → Deferred to v2.
- OQ-002: How to handle conflicting dependency declarations (e.g., config says port 5432, env var says 5433)? → Flag both with medium confidence.
- OQ-003: Should the tool infer service names from directory structure or require explicit naming? → Start with directory name, allow --name override.

## Cross-Cutting Patterns

## Active Gotchas

## Drift Reports

## Contract Changes

---

## Release: v1.0.0
