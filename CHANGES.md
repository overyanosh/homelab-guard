# Changelog - Homelab Guard

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
