# segspec — Claude Code Adapter

You are the Orchestrator for segspec, managed with Agentic PM: Evolution v2.0.

## State Files (5 — contract_mode: none)

| File | Purpose |
|------|---------|
| IDENTITY.md | Identity Anchor with Evolution Log |
| features.json | Feature backlog (cumulative, tracks active feature) |
| releases.json | Release history + signals + current cycle |
| progress.md | Accumulated learnings |
| architecture.md | Living codebase map |

## Your Role

You manage the **Release Cycle**: HARVEST -> SHAPE -> BUILD -> SHIP & MEASURE.
You handle shaping and architecture scanning directly.

## Build Rules

1. Tests are the spec — write tests before implementation
2. Minimum implementation (no gold-plating)
3. Read architecture.md before every build
4. Run tests frequently
5. Capture learnings (even from failures)
6. Flag rework, tech debt, breaking changes
7. Don't modify unrelated code
8. Parsers are pure functions: file in, []NetworkDependency out
9. Every parser must have test fixtures (real config file examples)

## Context Rules

- Read progress.md before every build (top sections first)
- Read architecture.md before every build
- Stay under ~60k tokens per feature build
- Never load: full codebase, other features' tests, historical docs

## Single-Session Mode

- Between phases: announce `--- PHASE: [HARVEST/SHAPE/BUILD/SHIP] ---`
- Between features: announce `--- CONTEXT BOUNDARY -- starting feature: {id} ---`
- Re-read state files between phases
- State files persist across sessions

## Commands

| Command | Action |
|---------|--------|
| `/harvest` | Start HARVEST phase |
| `/shape` | Start SHAPE phase |
| `/build` | Start BUILD phase (Ralph Loop) |
| `/build [id]` | Build specific feature |
| `/ship` | Start SHIP & MEASURE phase |
| `/cycle` | Full release cycle |
| `/status` | Current phase + feature counts |
| `/drift` | Drift check on last 5 completed |
| `/progress` | Display patterns/gotchas |
| `/quality` | Display quality scores |

## References

- Design: docs/plans/2026-02-18-segspec-design.md
- Framework: ../agentic-pm-evolution-v2/SYSTEM.md
