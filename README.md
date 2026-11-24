# 🛡️ Homelab Guard

**License:** Apache 2.0 | **Docker:** Multi-Arch (`amd64` / `arm64`) | **Security:** AES-256-GCM & mTLS

**Homelab Guard** is a unified, self-hosted control plane designed to manage your entire infrastructure from a single web interface—combining Docker/Swarm orchestration, Proxmox VE hypervisor control, a zero-trust Web SSH bastion, and an autonomous AI operations agent.

## ✨ Core Features

* **🐳 Docker & Swarm Management**: Multi-host container lifecycle, stacks/compose editor, live logs, network/volume inspection, and label-driven automated image updates via lightweight remote agents.
* **🖥️ Proxmox VE Integration**: Cluster-wide resource monitoring (CPU, RAM, ZFS/LVM storage), VM & LXC lifecycle management (start, stop, reboot, snapshots, Cloud-Init cloning) via scoped API tokens.
* **🔐 WebUI SSH Bastion & Key Vault**: Browser-based terminal (`xterm.js`) backed by an encrypted Ed25519 key vault—zero passwords, zero private keys exposed to the client browser.
* **🤖 Autonomous AI Ops Agent**: OpenAI-compatible agent (supports local **Ollama / LiteLLM**, OpenRouter, OpenAI) capable of generating `docker-compose.yml` stacks, managing `guard.*` labels, and running isolated non-regression tests before promoting container updates.

## 🚀 Quickstart

```bash
cd deploy
cp .env.example .env
# Edit .env with your generated MASTER_KEY (openssl rand -hex 32)
docker compose up -d --build
```
