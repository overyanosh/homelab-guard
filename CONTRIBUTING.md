# Contributing to Homelab Guard

Thank you for your interest in contributing to **Homelab Guard**!

## Prerequisites
* Go 1.23+
* Node.js 22+ & npm
* Python 3.12+
* Docker

## Coding Standards
* Format Go code with `gofmt` and ensure `go vet ./...` passes.
* Keep `CGO_ENABLED=0` compatibility for static `amd64`/`arm64` cross-compilation.
* Never log plaintext secrets, SSH private keys, or API tokens.
* Follow Conventional Commits (`feat:`, `fix:`, `docs:`, `sec:`, `chore:`).
