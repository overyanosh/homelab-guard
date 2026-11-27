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

## URL du serveur (piège classique)

`GUARD_SERVER_URL` accepte indifféremment :

| Forme | Exemple |
|---|---|
| http(s) (converti automatiquement en ws(s)) | `http://192.168.0.6:9015` |
| websocket explicite | `ws://192.168.0.6:9015` |
| hôte nu (ws par défaut) | `192.168.0.6:9015` |

Si l'agent tourne **sur la même machine** que le serveur (conteneurs séparés),
utilisez la passerelle de l'hôte :

```bash
docker run ... --add-host host.containers.internal:host-gateway \
  -e GUARD_SERVER_URL=http://host.containers.internal:9015 ...
```

### Vérifier qu'un agent se connecte

```bash
docker logs homelab-guard-agent          # doit afficher "Connected to control plane"
curl -s http://<serveur>:9015/api/nodes  # le nœud doit être online:true
```

Le **jeton d'enrôlement doit être créé sur l'instance cible** (celle qui doit
recevoir l'agent) : un jeton généré sur une autre instance est refusé
(`401 unauthorized agent token`).

## L'agent se connecte mais l'UI affiche « Cannot connect to the Docker daemon »

L'agent joint bien le serveur (`Connected to control plane`) mais **ne voit pas le
socket Docker** de sa machine : le conteneur agent a été lancé **sans le montage**.

```bash
docker run -d --name homelab-guard-agent --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \   # ← indispensable
  -e GUARD_SERVER_URL=http://192.168.0.6:9015 \
  -e GUARD_ENROLL_TOKEN=<jeton du NAS> \
  ghcr.io/overyanosh/homelab-guard-agent:test
```

Depuis la version 0.9.19, l'agent :

- **détecte automatiquement** le socket (`/var/run/docker.sock`, socket rootless
  `/run/user/<uid>/docker.sock`, socket Podman…) sauf si `DOCKER_HOST` est défini ;
- journalise `Docker endpoint: …` et, si aucun socket n'est trouvé, la liste des
  chemins testés ;
- renvoie à l'UI un message **actionnable** (monter le socket / définir
  `DOCKER_HOST` / ajouter le groupe docker) au lieu du message brut.

Cas particuliers :

| Situation | Solution |
|---|---|
| Docker **rootless** | `-e DOCKER_HOST=unix:///run/user/<uid>/docker.sock` |
| Podman | `-v /run/user/<uid>/podman/podman.sock:/var/run/docker.sock` |
| `permission denied` sur le socket | `--group-add $(stat -c %g /var/run/docker.sock)` ou `--user 0:0` |

Exemple concret (socket `root:docker` gid **988**) :

```bash
sudo docker run -d --name homelab-guard-agent --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add 988 \
  -e GUARD_SERVER_URL=http://192.168.0.6:9015 \
  -e GUARD_ENROLL_TOKEN=<jeton du NAS> \
  ghcr.io/overyanosh/homelab-guard-agent:test
```

L'agent doit loguer `Docker API reachable (unix:///var/run/docker.sock)`
(version 0.9.20+). Sinon il indique précisément s'il manque le montage ou les
droits.

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
