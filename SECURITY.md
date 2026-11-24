# Security Policy

## Security Architecture
* **Encrypted Secret Vault (`AES-256-GCM`)**: SSH keys and Proxmox tokens are encrypted at rest in SQLite using `MASTER_KEY`.
* **Zero-Trust Web SSH Bastion**: SSH private keys are decrypted strictly in server memory and never sent to the browser.
* **Outbound-Only Agent Connectivity (`mTLS` / `WSS`)**: Remote Docker/Swarm agents connect outbound using hashed enrollment tokens (`SHA-256`) and optional client certificates.
* **Read-Only Sandbox Testing**: Automated container regression tests mount volumes in `:ro` mode.

## Reporting a Vulnerability
Please use **GitHub Private Vulnerability Reporting** under **Security -> Advisories** rather than opening a public issue.
