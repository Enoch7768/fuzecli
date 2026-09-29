# FuzeCLI Documentation

This directory contains the technical documentation for FuzeCLI.

## Core documents

- [Architecture](ARCHITECTURE.md) — system boundaries, major components, and design direction.
- [API](API.md) — local HTTP API behavior, endpoints, authentication, and workspace access.
- [Web Studio](WEB.md) — embedded web interface and Studio workflow.
- [Terminal](TERMINAL.md) — interactive CLI behavior and commands.
- [MCP](MCP.md) — MCP server behavior and client integration.
- [Security](SECURITY.md) — security model, workspace isolation, credential handling, API protections, and threat-model limitations.

## Project-level documents

- [README](../README.md) — installation, quick start, features, and project overview.
- [Contributing](../CONTRIBUTING.md) — contribution workflow and quality requirements.
- [Code of Conduct](../CODE_OF_CONDUCT.md) — community participation standards.
- [Security reporting](../SECURITY.md) — vulnerability reporting guidance.
- [Support](../SUPPORT.md) — troubleshooting, bug reports, and feature requests.
- [Changelog](../CHANGELOG.md) — release and development history.

## Documentation principles

Documentation should describe behavior that actually exists in the repository. When behavior changes, update the relevant documentation in the same change whenever practical.

Security-sensitive behavior should be documented together with its implementation and covered by regression tests where appropriate.
