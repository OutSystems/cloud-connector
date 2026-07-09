---
paths:
  - "main.go"
  - ".goreleaser.yaml"
---

<!-- Proposed from inner-sourcing analysis of merged PR #138 — edit freely. -->

# Versioning: `main.version` is only real in release builds

`main.go` declares `var version = "dev" // Set by goreleaser`. In source, in `go run`, and in any plain `go build`, `version` stays `"dev"`. A real version string can only come from the release pipeline (`.goreleaser.yaml` -> the Docker image published to GHCR) injecting it at link time — and only when `.goreleaser.yaml` is actually configured to do so.

Treat the `// Set by goreleaser` comment as a claim to verify, not a guarantee. Injection requires `-X main.version={{ .Version }}` in the build entry's `ldflags` in `.goreleaser.yaml`. **At HEAD that linker flag is absent**: PR #138 added `ldflags: ["-s -w -X main.version={{ .Version }}"]`, but commit 5419c5f ("dependabot updates and go version bump") removed it, so the current build entry carries no `ldflags` at all. As a result every released client currently reports `dev` — the failure mode below is live, not hypothetical. Adding custom `ldflags`/`flags` to the build without re-adding `-X main.version=...` keeps injection dropped.

## When you make `version` load-bearing

`version` is already load-bearing: `main.go` sets the connection `User-Agent` to `CloudConnector/<version>` so Support can identify the client version from access logs. If you surface `version` anywhere else production depends on — a request header, a log line, a metric, anything Support/ops reads off the wire (not just the `--help` text) — you MUST confirm the release build actually injects it:

- Open `.goreleaser.yaml` and check the build entry explicitly carries `-X main.version={{ .Version }}` in its `ldflags`. Add it back if it's missing (it is, at HEAD). Do not rely on it being injected implicitly.
- Understand the failure mode: it is silent. The code compiles, `go test` passes (it runs against `"dev"`), and the header/log is emitted — yet every released client advertises `CloudConnector/dev`, making the value useless. Unit tests cannot catch this; only a release/snapshot build can. Verify with `goreleaser --clean --snapshot` (or the published image) and inspect the actual emitted value, not just `go test`.

Keep the source default `"dev"` — it is the correct sentinel for local/dev runs.
