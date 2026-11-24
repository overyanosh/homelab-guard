# Architecture Decision Records (ADR) — Homelab Guard

* **ADR-001**: Go backend + Python FastAPI AI worker + React/TypeScript SPA embedded via `go:embed` in a single multi-stage container.
* **ADR-002**: Zero-trust Web SSH bastion—private keys never leave server memory.
* **ADR-003**: Outbound-only WebSocket/mTLS remote Docker/Swarm agent (`distroless:nonroot`).
* **ADR-004**: 4-phase transactional container upgrade pipeline (*Pull -> Read-Only Sandbox -> Regression Verification -> Promotion or AI-Diagnosed Rollback*).
