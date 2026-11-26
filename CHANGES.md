# Changelog - Homelab Guard

## Version 0.4.0 (2026-11-26)

### 🎉 Compose par service + renommage du menu

- Le menu « Docker & Swarm » devient **« Podman, Docker & Swarm »** (idem pour
  l'en-tête de page et le titre des réglages).
- Nouvelle colonne **Compose** dans le tableau des conteneurs (juste avant
  « Mise à jour »), avec une icône ouvrant une modale d'**affichage / édition**
  du `docker-compose` du service.
  - Si aucune définition n'est enregistrée, un compose est **généré depuis
    l'inspection du conteneur** (image, restart, ports, variables
    d'environnement, volumes).
  - Les variables sensibles (`*KEY*`, `*SECRET*`, `*TOKEN*`, `*PASSWORD*`) sont
    **masquées** (`***`) dans l'aperçu.
  - Le compose peut être **modifié puis enregistré** (persisté en SQLite par
    service).

### 🔧 Backend
- `GET /api/docker/containers/{id}/compose` (généré ou enregistré)
- `PUT /api/docker/containers/{id}/compose` (enregistre la définition)
- Table `compose_files` (service, yaml, updated_at)

## Version 0.3.0 (2026-11-26)

### 🎉 Pages de réglages par module

Chaque section du menu latéral dispose désormais d'une icône d'engrenage (⚙) en fin
de ligne, ouvrant une page de réglages dédiée.

#### Docker & Swarm
- Formulaire de **déploiement d'un nouvel agent** Docker ou Podman (nom, runtime,
  URL du serveur Guard).
- Génération du **jeton d'enrôlement** (affiché une seule fois) et de la commande
  `docker/podman run` prête à coller, avec bouton « Copier ».
- Liste des agents enregistrés avec état en ligne/hors ligne et suppression.
- **L'agent local est protégé** : impossible à supprimer (badge « local »).

#### Proxmox VE
- Formulaire de **nouvelle connexion** (nom, endpoint, token ID/secret, TLS).
- La connexion est testée avant enregistrement.
- Liste des connexions enregistrées avec suppression.

#### Bastion SSH
- Formulaire de **nouvelle connexion SSH** (nom, hôte, port, utilisateur).
- Génération automatique d'une paire de clés Ed25519 ; la clé publique est
  affichée (avec bouton « Copier ») pour l'ajouter à `authorized_keys`.
- Liste des hôtes enregistrés avec suppression.

#### Agent IA & Ops
- Page d'état du worker IA : en ligne/hors ligne, modèle et endpoint.
- Bouton « Tester » (via `GET /api/ai/config`).

### 🔧 Backend
- `DELETE /api/nodes/{id}` (protège l'agent local)
- `DELETE /api/proxmox/clusters/{id}`
- `DELETE /api/ssh/hosts/{id}`
- `GET /api/ai/config` (état + configuration non sensible du worker)
- `GET /health` ajouté au worker IA

## Version 0.2.0 (2026-11-26)

### 🎉 Nouvelles Fonctionnalités

#### 1. Support Multi-Runtime (Docker & Podman) ✅
Le backend détecte automatiquement et supporte à la fois les runtime Docker et Podman sur le même nœud local ou distant.

**Améliorations apportées :**
- Nouveau module `internal/podman/client.go` qui tente de se connecter aux sockets Podman rootless
- Endpoint `/api/docker/containers` retourne maintenant des conteneurs de tous les runtimes détectés
- Chaque conteneur est étiqueté avec son runtime (`docker`, `podman`) dans l'interface UI

**Configuration automatique :**
```go
// Le serveur tente automatiquement ces chemins de socket :
/run/user/$UID/podman/podman.sock
/run/podman/podman.sock
/var/run/podman/podman.sock
/run/docker.sock
```

#### 2. Visualisation des Logs des Conteneurs ✅
Nouvelle fonctionnalité permettant de visualiser les 100 dernières lignes des logs de n'importe quel conteneur depuis l'interface web.

**Interface utilisateur :**
- Colonne "Logs" ajoutée au tableau des conteneurs
- Bouton œil (Eye icon) pour ouvrir la modale de logs
- Modale responsive affichant les logs en format monospace
- Chargement asynchrone avec indicateur de progression

**API endpoint :**
```http
GET /api/docker/containers/{id}/logs
```

#### 3. Détection Automatique de Versions Disponibles ✅
Le système vérifie automatiquement si une version plus récente existe pour chaque image de conteneur en interrogeant Docker Hub ou GitHub Container Registry.

**Fonctionnalités :**
- Badge vert avec icône `PackageCheck` indiquant la disponibilité d'une nouvelle version
- Colonne "Mise à jour" dans le tableau des conteneurs
- Vérification asynchrone pendant le chargement des conteneurs
- Support pour Docker Hub et ghcr.io (extension facile vers d'autres registries)

**API endpoint :**
```http
POST /api/docker/images/check-update
Content-Type: application/json
{
  "image_name": "nginx:latest"
}
```

### 🔧 Améliorations Techniques

#### Backend Go
- **File créée:** `backend/internal/podman/client.go`
  - Client wrapper pour Podman compatible avec Docker API
  - Tentative multiple de connection aux sockets Podman
  - Mécanisme de fallback élégant
  
- **Fichier modifié:** `backend/cmd/server/main.go`
  - Détection automatique du runtime au démarrage
  - Endpoints enrichis pour logs et check version
  - Gestion propre des erreurs multi-runtime

#### Frontend React/TypeScript
- **Fichier modifié:** `frontend/src/App.tsx`
  - Interface table élargie avec 8 colonnes
  - État géré pour modale logs et versions récentes
  - Composants Lucide React supplémentaires (`Eye`, `PackageCheck`)
  - Style amélioré avec Tailwind CSS

### 📦 Structures de Données

#### ContainerItem (Frontend)
```typescript
interface ContainerItem {
  id: string;
  names: string[];
  image: string;
  state: string;
  status: string;
  guard: {
    enabled: boolean;
    group: string;
    update_mode: string;
    test_strategy: string;
  };
  runtime: string;                    // ← NOUVEAU
  newer_version_available?: boolean;  // ← NOUVEAU
}
```

### 🎨 Interface Utilisateur

#### Nouvelle Table des Conteneurs
| Colonne       | Description | Icones |
|--------------|-------------|--------|
| Service      | Nom du conteneur | - |
| Image        | Reference de l'image | - |
| État         | Running/Stopped/etc. | ● Rouge/Jaune/Vert |
| Moteur       | Runtime utilisé | - |
| Mode/Test    | Configuration guard.* | - |
| Mise à jour  | Badge vert si mise à jour disponible | ✓ PackageCheck |
| Logs         | Accès rapide aux logs | 👁 Eye |
| Actions      | Bouton Test & Update | → |

### 🛠️ Utilisation

#### Consulter les logs
1. Cliquer sur l'icône œil dans la colonne "Logs"
2. La modale s'ouvre avec les 100 dernières lignes
3. Scroller vertical pour voir plus de contenu
4. Fermer avec la croix ou cliquer en dehors

#### Voir les mises à jour disponibles
- Les badges verts apparaissent automatiquement après le chargement
- Actualiser (⭕ Refresh) pour forcer une nouvelle vérification

#### Support Mixte Docker/Podman
- Aucune configuration nécessaire
- Détection automatique au démarrage
- Tous les conteneurs affichés ensemble

### 📋 Notes de Migration

Pour les utilisateurs existants :
- Aucune action requise
- Compatible rétroversionnelle
- Les données existantes (SSH hosts, Proxmox clusters, etc.) restent intactes

### 🔮 Vers la Version Suivante

- [ ] Support pour registries privés avec authentification
- [ ] Export des logs en fichier .log
- [ ] Alertes email/notification pour nouvelles versions
- [ ] Filtrage par runtime dans l'UI
- [ ] Tests unitaires pour nouveaux endpoints

---

*Développé avec ❤️ pour la communauté Homelab*
