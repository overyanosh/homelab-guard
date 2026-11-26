# Déploiement Homelab Guard avec Podman

## Prérequis

- Podman 5.x installé
- Python 3.12+ pour l'AI worker (si utilisé)
- Accès à un serveur AI (Ollama, LiteLLM, etc.) sur le port 11434

## Étapes de déploiement

### 1. Vérifier les environnements

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

### 3. Construire l'image

```bash
cd /home/drfataliste/homelab-guard/homelab-guard/deploy
podman build -t homelab-guard .. -f ../Dockerfile
```

### 4. Lancer le conteneur

```bash
podman run -d \
  --name homelab-guard \
  -p 8443:8443 \
  -v $(pwd)/data:/data \
  -e MASTER_KEY=$MASTER_KEY \
  -e AI_BASE_URL=http://host.containers.internal:11434/v1 \
  homelab-guard
```

### 5. Optionnel: Utilisation du socket Docker/Podman

Si vous avez besoin d'accéder au socket Docker/Podman pour gérer des conteneurs depuis Homelab Guard:

**Option A - Sans socket (mode limité)**
```bash
podman run -d \
  --name homelab-guard \
  -p 8443:8443 \
  -v $(pwd)/data:/data \
  homelab-guard
```

**Option B - Avec socket Podman (nécessite root ou permissions spéciales)**
```bash
podman run -d \
  --name homelab-guard \
  -p 8443:8443 \
  -v /run/user/1000/podman/podman.sock:/var/run/docker.sock:ro \
  -v $(pwd)/data:/data \
  homelab-guard
```

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
# Tirer la nouvelle version
podman pull ghcr.io/overyanosh/homelab-guard:latest

# Rebuild et relancer
podman stop homelab-guard && podman rm homelab-guard
podman run -d --name homelab-guard -p 8443:8443 -v $(pwd)/data:/data ...
```

## Résolution de problèmes

### Erreur "connection refused" pour AI Base URL
- Assurez-vous que votre serveur AI (Ollama, etc.) tourne sur le port 11434
- Testez: `curl http://localhost:11434/api/tags`

### Erreur de socket Docker
- Homelab Guard peut fonctionner sans socket (gestion basique uniquement)
- Pour le Swarm/Docker remote management, utilisez `podman system service --time=0 &`

### Ports déjà utilisés
- Changez le port: `-p 9443:8443` au lieu de `-p 8443:8443`
