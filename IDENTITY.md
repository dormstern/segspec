# Identity Anchor

## Primary Persona

**WHO**: Platform engineer at a mid-market enterprise (1,000-10,000 employees) operating Kubernetes clusters, responsible for implementing network segmentation as part of Zero Trust initiatives.

**JOB**: When I need to create microsegmentation policies for my applications, I want to point a CLI tool at my app configs and binaries and get NetworkPolicy YAML out, so that I can enforce segmentation in hours instead of weeks of manual traffic analysis and cross-team coordination.

**NEVER**: This product must never become a full microsegmentation platform, a runtime traffic monitoring agent, an enterprise SaaS with accounts/dashboards, or a tool that requires network access or API keys to function.

## Architecture Constraints

- Go: single binary distribution, zero runtime dependencies
- CLI-only: no web UI, no API server, no daemon process
- Zero external services: no API keys, no accounts, no telemetry, fully offline
- K8s NetworkPolicy YAML as primary output format (other formats in future releases)
- Parsers are pure functions: file in, []NetworkDependency out, independently testable

## Evolution Log

### v1.0 (2026-02-18) -- Genesis
- Initial identity. Derived from Agentic Founder thesis validation (AI-Native Microsegmentation) and brainstorming session identifying whitespace: no tool combines app analysis with network policy generation.
