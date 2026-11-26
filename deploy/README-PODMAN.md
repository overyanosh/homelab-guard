# Déploiement Homelab Guard avec Podman

## Prérequis

- Podman 5.x installé
- Python 3.12+ pour l'AI worker (si utilisé)
- Accès à un serveur AI (Ollama, LiteLLM, etc.) sur le port 11434

## Étapes de déploiement

### 1. Vérifier l'environnement

```bash
podman --version
python3 --version
```

### 2. Configuration

Le fichier `.env` doit contenir:
```bash
MASTER_KEY=<64 caractères hexadécimaux>
AI_BASE_URL=http://host.containers.internal:11434/v1
AI_API_KEY=sk-local
AI_MODEL=qwen2.5-coder:14b
```

### 3. Activer le socket Podman (API Docker-compatible)

Homelab Guard utilise l'API Docker-compatible de Podman pour lister/gérer
les conteneurs locaux. Il faut activer le socket rootless :

```bash
systemctl --user enable --now podman.socket
loginctl enable-linger $USER   # pour survivre à la déconnexion / au reboot
```

Le socket est alors disponible sur `$XDG_RUNTIME_DIR/podman/podman.sock`
(généralement `/run/user/1000/podman/podman.sock`).

### 4. Construire l'image

```bash
cd /home/drfataliste/homelab-guard/homelab-guard/deploy
podman build -t homelab-guard .. -f ../Dockerfile
```

### 5. Lancer le conteneur

```bash
podman run -d \
  --name homelab-guard \
  --restart=unless-stopped \
  --userns=keep-id:uid=10001,gid=10001 \
  --security-opt label=disable \
  -p 9015:9015 \
  -v $(pwd)/../data:/data \
  -v /run/user/1000/podman/podman.sock:/var/run/docker.sock \
  -e DOCKER_HOST=unix:///var/run/docker.sock \
  -e MASTER_KEY=$MASTER_KEY \
  -e AI_BASE_URL=http://host.containers.internal:11434/v1 \
  homelab-guard
```

Ou avec compose :

```bash
cd /home/drfataliste/homelab-guard/homelab-guard/deploy
podman compose -f docker-compose-podman.yml up -d --build
```

> **Note rootless / permissions**
> Le conteneur tourne avec l'utilisateur `guard` (uid 10001). L'option
> `--userns=keep-id:uid=10001,gid=10001` mappe cet uid sur l'utilisateur hôte
> (uid 1000), ce qui lui permet de lire le socket Podman (mode `0660`,
> propriétaire hôte). Les fichiers de `../data` doivent appartenir à l'uid hôte.
> Si besoin, corriger avec : `podman unshare chown -R 0:0 ../data`.

### 6. Superviser

```bash
# Voir les logs
podman logs -f homelab-guard

# Voir les conteneurs
podman ps

# Arrêter
podman stop homelab-guard

# Supprimer
podman rm homelab-guard
```

### 7. Mise à jour

```bash
podman stop homelab-guard && podman rm homelab-guard
podman build -t homelab-guard .. -f ../Dockerfile
podman run -d ... homelab-guard   # même commande qu'à l'étape 5
```

## Résolution de problèmes

### L'interface ne montre aucun conteneur local
- Vérifier que le socket Podman est actif :
  `systemctl --user status podman.socket`
- Vérifier que le socket est monté et lisible dans le conteneur :
  `podman exec homelab-guard ls -l /var/run/docker.sock`
- Vérifier l'accès à l'API :
  `curl -s --unix-socket /run/user/1000/podman/podman.sock http://d/v1.40/containers/json`

### Erreur "Cannot connect to the Docker daemon"
- Le socket Podman n'est pas monté, ou `DOCKER_HOST` n'est pas défini.
- Relancer avec `-v /run/user/1000/podman/podman.sock:/var/run/docker.sock`
  et `-e DOCKER_HOST=unix:///var/run/docker.sock`.

### Erreur "connection refused" pour AI Base URL
- Assurez-vous que votre serveur AI (Ollama, etc.) tourne sur le port 11434
- Testez: `curl http://localhost:11434/api/tags`

### Ports déjà utilisés
- Changez le port hôte: `-p 9443:9015` au lieu de `-p 9015:9015`
