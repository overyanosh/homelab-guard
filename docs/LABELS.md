# Spécification des Labels `guard.*`

| Label | Valeurs / Format | Défaut | Description |
| :--- | :--- | :--- | :--- |
| `guard.enable` | `true` \| `false` | `false` | Active la prise en charge par Homelab Guard. |
| `guard.group` | Chaîne | `default` | Regroupe les services dans l'interface. |
| `guard.update.mode` | `manual` \| `notify` \| `auto` | `notify` | Politique de mise à jour. |
| `guard.update.schedule` | Cron | `0 3 * * *` | Fenêtre temporelle autorisée. |
| `guard.test.strategy` | `healthcheck` \| `http` \| `tcp` \| `exec` | `healthcheck` | Stratégie de validation sandbox. |
| `guard.test.endpoint` | URL ou `host:port` | — | Cible HTTP (2xx) ou TCP. |
| `guard.test.cmd` | Commande shell | — | Commande exécutée dans le sandbox (`exec`). |
| `guard.test.timeout` | Durée (`60s`) | `60s` | Timeout maximum du test. |
| `guard.ai.context` | Texte libre | — | Contexte métier transmis à l'agent IA. |
| `guard.backup.pre-update` | `true` \| `false` | `false` | Déclenche un snapshot Proxmox avant mise à jour. |
