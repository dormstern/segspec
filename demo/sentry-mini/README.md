# sentry-mini — segspec demo fixture

A synthesized, license-clean approximation of a Sentry self-hosted topology
for use as a `segspec analyze --demo sentry-mini` target. Inspired by the
publicly documented Sentry architecture (web, worker, cron, relay, snuba,
symbolicator, vroom, kafka, postgres, redis, clickhouse, memcached, smtp).

**No file in this directory was copied from `getsentry/self-hosted` or
`sentry-kubernetes/charts`.** Service names and ports follow public
documentation; values, defaults, and structure are synthesized.

Files:
- `docker-compose.yml` — multi-service Compose stack (~17 services).
- `values.yaml` — Helm-style values snippet covering postgres / redis / kafka / clickhouse subcharts.
- `.env` — environment-variable form of the same dependency graph (segspec parses .env files too).

Inspiration / credit: https://github.com/getsentry/self-hosted (BSL 1.1).
