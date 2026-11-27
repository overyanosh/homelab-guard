# Déploiement sur un appareil distant (SSH + Docker)

Script : [`deploy/remote-deploy.sh`](../deploy/remote-deploy.sh)

## Principe

L'image est construite **localement**, puis transférée par
`podman save | ssh 'docker load'` : **aucun registry, aucun login GHCR** n'est
nécessaire sur la machine distante.

## Deux modes

### `agent` — recommandé

Déploie uniquement l'agent, qui s'enregistre auprès de cette instance et
remonte ses conteneurs (socket Docker monté).

```bash
# 1. Créer le jeton dans l'UI : Podman/Docker & Swarm -> ⚙ -> "Déployer un nouvel agent"
# 2. Déployer
GUARD_ENROLL_TOKEN=hlg_xxx \
SSH_KEY=~/.ssh/id_ed25519 \
./deploy/remote-deploy.sh agent user@host
```

### `full` — instance complète et indépendante

Déploie un second Homelab Guard (port 9015, sa propre base SQLite). Il ne voit
que les conteneurs de la machine distante.

```bash
MASTER_KEY=$(openssl rand -hex 32) \
SSH_KEY=~/.ssh/id_ed25519 \
./deploy/remote-deploy.sh full user@host
```

> Conservez la `MASTER_KEY` : sans elle, les secrets stockés dans la base
> distante (agents, SSH, Proxmox) sont définitivement illisibles.

## Exemple réel (NAS ASUSTOR « Hakobune »)

```bash
MASTER_KEY=$(grep MASTER_KEY ~/.config/homelab-guard/remote-hakobune.env | cut -d= -f2) \
SSH_KEY=~/.ssh/id_ed25519 \
./deploy/remote-deploy.sh full drfataliste@192.168.0.6
```

Résultat :

- `http://192.168.0.6:9015` → UI, version affichée en bas de la barre latérale
- moteur détecté : **`docker`** (via `/version`), 54 conteneurs listés
- réseaux multiples correctement détectés (`['ai', 'proxy']`)
- « Local Host » enregistré et en ligne

## Notes d'exploitation

- Le conteneur tourne avec `--user 0:0` : le socket Docker du NAS est
  `root:docker (0660)` et le volume nommé `/data` appartient à root. Pour
  durcir : `--group-add <gid du groupe docker>` + `chown 10001:10001` sur le
  volume.
- Mise à jour : relancer la même commande (le conteneur est recréé).
- Suppression :
  ```bash
  ssh user@host 'docker rm -f homelab-guard && docker volume rm homelab-guard-data'
  ```
- L'instance distante est **indépendante** : pour une supervision croisée,
  préférez le mode `agent`.
