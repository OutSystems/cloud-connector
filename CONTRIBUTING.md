# Contributing to OutSystems Cloud Connector

## Prerequisites

- **Go 1.25.1+** -- see [go.dev/dl](https://go.dev/dl/) for installation
- **Git**
- **Docker** (optional, for building container images)
- **GoReleaser** (optional, for testing release builds locally)

## Getting Started

Clone the repository and download dependencies:

```bash
git clone https://github.com/OutSystems/cloud-connector.git
cd cloud-connector
go mod download
```

## Building

Build the binary:

```bash
go build -o outsystemscc .
```

Test a release build locally with GoReleaser (produces Linux binaries and Docker images):

```bash
goreleaser --clean --snapshot --config .goreleaser.yaml
```

Build artifacts land in `dist/`.

## Running Tests

```bash
go test ./...
```

Tests use [httpmock](https://github.com/jarcoal/httpmock) for HTTP mocking. No external services or credentials are needed.

## Code Style

There is no project-level linter configuration checked in. Follow standard Go conventions:

- Run `go vet ./...` before submitting.
- Run `gofmt -s -w .` to format code.
- Keep imports grouped: stdlib, then external packages (the standard `goimports` ordering).
- The project is a single `main` package -- keep it that way unless there is a strong reason to refactor.

## Branch Naming and PR Process

Branch names follow the pattern `<JIRA-ID>` or `feature/<JIRA-ID>` (e.g., `RDODCP-283`, `feature/RDGRS-811`). Use descriptive names when there is no associated ticket.

### Workflow

1. Create a branch off `main`.
2. Make your changes and ensure tests pass (`go test ./...`).
3. Push and open a pull request targeting `main`.
4. PRs require review from the code owners defined in [`.github/CODEOWNERS`](.github/CODEOWNERS): **@OutSystems/global-routing-and-security** and **@OutSystems/cloud-enablement-services**.
5. After approval, merge via the GitHub UI.

## Releases

Releases are managed with [GoReleaser](https://goreleaser.com/) and follow semantic versioning (tags like `v2.0.3`). The release pipeline:

1. Runs `go mod tidy` and `go generate ./...` as pre-build hooks.
2. Cross-compiles Linux binaries for `amd64`, `arm64`, and `386`.
3. Builds and pushes a Docker image to `ghcr.io/outsystems/outsystemscc`.
4. Generates a changelog (excluding `docs:` and `test:` prefixed commits).

## Dependency Management

Go modules (`go.mod` / `go.sum`) manage dependencies. Dependabot is configured to open monthly update PRs for Go modules. The project uses a forked version of chisel (`github.com/outsystems/chisel`) via a `replace` directive in `go.mod`.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
