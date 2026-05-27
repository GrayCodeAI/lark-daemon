# Contributing to Lark

Thank you for your interest in contributing to Lark. This document explains how to get started.

## Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/<your-username>/lark-daemon.git
   cd lark-daemon
   ```
3. Create a branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```
4. Make your changes
5. Push and open a pull request

## Development Setup

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose (optional)
- `golangci-lint` for linting (optional)

### Building

```bash
make build
```

### Running

```bash
make run
```

### Testing

```bash
make test
```

Tests use in-memory SQLite by default, so no external database is required.

### Linting

```bash
make lint
```

## Code Style

- Follow standard Go conventions and `gofmt` formatting
- Keep handler files focused on a single domain (see `internal/server/api/`)
- Use `slog` for structured logging
- All API responses must use the `writeJSON` helper
- All API errors must use structured error codes from `errors.go`
- New endpoints must have corresponding tests

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat: add workspace invite links
fix: prevent duplicate channel membership
docs: update environment variable table
test: add coverage for webhook execution
```

## Pull Requests

- Keep PRs focused on a single change
- Include a clear description of what changed and why
- Ensure all tests pass (`make test`)
- Ensure the linter passes (`make lint`)
- Add tests for new functionality
- Update documentation if the API changes

## Reporting Issues

- Use GitHub Issues for bug reports and feature requests
- Include reproduction steps for bugs
- Include the Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
