# Self-Serve Gaps: What's Missing Before "Go Viral"

## Critical for Reddit/LinkedIn Launch

### 1. Release binaries don't exist yet
The SHOWOFF.md references `curl` install from GitHub releases, but we haven't set up GoReleaser or published any binaries. Need:
- GoReleaser config (`.goreleaser.yml`)
- GitHub Actions workflow for release builds
- Binaries for: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64
- Priority: **BLOCKER**

### 2. No `segspec analyze <github-url>` support
Network/security engineers may not have app repos cloned. They want to type:
```bash
segspec analyze https://github.com/org/repo
```
and have it clone + scan automatically. This is a 30-line feature (detect URL, git clone to tempdir, analyze, cleanup).
- Priority: **HIGH** — removes friction for first-time users

### 3. Helm template resolution
Many real K8s projects use Helm charts, not raw manifests. segspec currently can't read `{{ .Values.redis.host }}`. Options:
- Shell out to `helm template` before analysis
- Add `--helm-values values.yaml` flag
- Priority: **HIGH** — without this, many real projects will show 0 results

### 4. Per-service NetworkPolicy output
Current output is one aggregate policy. Practitioners need one NetworkPolicy per service with proper `podSelector` matching. Without this, the output isn't directly usable in production.
- Priority: **HIGH** — the current output is a good demo but not production-ready

### 5. No README.md on the GitHub repo
The repo has no README. Anyone clicking the GitHub link from Reddit will see raw code with no explanation.
- Priority: **BLOCKER** — copy/adapt SHOWOFF.md into README.md

## Nice-to-Have Before Launch

### 6. Docker image
Some practitioners don't want to install binaries:
```bash
docker run --rm -v $(pwd):/app segspec analyze /app
```
- Priority: **MEDIUM**

### 7. GitHub Action
```yaml
uses: dormstern/segspec-action@v1
with:
  path: .
  format: netpol
```
- Priority: **MEDIUM** — good for the CI/CD pitch

### 8. Pretty terminal output
Current output is plain text. Colors, tables, or even a simple box drawing would make screenshots more shareable on social media.
- Priority: **LOW** for functionality, **MEDIUM** for virality

### 9. JSON output format
For programmatic consumption and integration with other tools.
- Priority: **LOW**

## Not Needed for Launch

- Multi-format output (AWS SGs, Cilium) — roadmap, not v1
- Interactive mode — nice but complex
- Web UI — violates Identity Anchor (CLI only)
