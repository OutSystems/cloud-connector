# CLAUDE.md

## Project Overview

OutSystems Cloud Connector (`outsystemscc`) is a Go CLI tool that creates reverse tunnels between private network endpoints and OutSystems Developer Cloud (ODC) Private Gateways. See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full system design and component breakdown.

## Quick Reference

```bash
# Build
go build -o outsystemscc .

# Test
go test ./...

# Lint / format
go vet ./...
gofmt -s -w .

# Snapshot release build (local)
goreleaser --clean --snapshot --config .goreleaser.yaml
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full development workflow, PR process, and release details.

## Directory Structure

```
cloud-connector/
  main.go              -- All application logic: CLI parsing, URL resolution, remote
                          validation, query parameter generation, chisel client setup
  main_test.go         -- Unit tests (uses httpmock, no external services needed)
  go.mod / go.sum      -- Go module; note the `replace` directive pinning chisel
                          to the OutSystems fork (github.com/outsystems/chisel)
  Dockerfile           -- Minimal Alpine image with static binary at /app/outsystemscc
  .goreleaser.yaml     -- Release config: Linux binaries (386/amd64/arm64) + Docker image
  .github/CODEOWNERS   -- PR reviewers: global-routing-and-security, cloud-enablement-services
  .github/dependabot.yml -- Monthly Go module dependency updates
  FAQ.md               -- Deployment examples (Azure Container Instances, network setup)
  README.md            -- User-facing documentation, usage examples, firewall setup
  images/              -- Assets for README
  dist/                -- Build artifacts (gitignored)
```

## Key Patterns and Conventions

- **Single `main` package**: The entire application is two files (`main.go` and `main_test.go`). Do not introduce additional packages without strong justification.
- **Thin wrapper over chisel**: Application logic is intentionally minimal. The tunneling engine (WebSocket transport, SSH encryption, port forwarding) is delegated to the chisel fork. Avoid duplicating functionality that chisel already provides.
- **`replace` directive in go.mod**: The import path uses `github.com/jpillora/chisel` but the actual code comes from `github.com/outsystems/chisel`. This is intentional -- do not remove the `replace` directive.
- **Version injection**: The `version` variable in `main.go` is set at build time by GoReleaser via `-ldflags`. The default value `"dev"` is used during local development.
- **HTTP redirect handling**: `fetchURL` uses a no-redirect policy and manually follows 302s to resolve the final tunnel endpoint URL. This is deliberate, not accidental.
- **Test mocking**: Tests use `httpmock` with `ActivateNonDefault` on the resty client. Follow this pattern for any new HTTP tests.

## Domain Terminology

| Term | Meaning |
|---|---|
| **Private Gateway** | ODC-side endpoint that accepts reverse tunnel connections, one per stage |
| **Remote** | A port-forwarding rule in the format `R:<local-port>:<remote-host>:<remote-port>` |
| **Token** | Authentication credential issued by the ODC Portal, passed as an HTTP header |
| **Address** | Server URL from the ODC Portal; may return a 302 redirect to the actual endpoint |
| **Session ID** | Random 9-digit integer appended as a query parameter to identify the connection |
| **Local port** | The port on the Private Gateway side (not the client side) that maps to the remote endpoint |

## Common Pitfalls

- **Linux-only binaries**: GoReleaser is configured to build only for Linux (`goos: linux`). Windows/macOS users run via WSL or Docker. Do not add other OS targets without product decision.
- **Duplicate local ports are rejected**: `validateRemotes` enforces unique local ports across all remote definitions. Two remotes cannot share the same local port even if they point to different hosts.
- **No inbound firewall rules**: The tool initiates all connections outbound. Never introduce logic that listens on a port or requires inbound connectivity.
- **Proxy passthrough**: The `--proxy` flag configures both the resty HTTP client (for URL resolution) and the chisel client (for the tunnel). Both must go through the same proxy.
- **`--hostname` is deprecated**: The flag still exists for backward compatibility but is ignored. It prints a deprecation warning. Do not remove it without a major version bump.
- **PID file path**: `generatePidFile` writes to the current working directory, not a configurable path. This is intentional for container environments.
- **No linter config checked in**: `.editorconfig` and `.markdownlint.json` are gitignored. Use `go vet` and `gofmt` as the baseline.

## Related Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) -- System context, component breakdown, connection lifecycle, security model
- [CONTRIBUTING.md](./CONTRIBUTING.md) -- Prerequisites, build/test commands, code style, branch naming, PR process, releases
- [FAQ.md](./FAQ.md) -- Deployment examples for Azure Container Instances
- [README.md](./README.md) -- User-facing usage guide, firewall requirements, examples
