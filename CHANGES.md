# Changelog - Homelab Guard

## Version 0.8.1 (2026-11-27)

### 🎉 Variables `.env` manquantes : clés ajoutées automatiquement

Lorsqu'un compose référence une variable (`${SECRET_KEY}`, `$API_TOKEN`) qui
n'existe pas dans le `.env`, la modale « Nouveau conteneur » :

1. **ajoute automatiquement la clé** au `.env` avec une valeur vide
   (`SECRET_KEY=`), active l'onglet `.env` et affiche un **compteur orange** sur
   cet onglet ;
2. affiche un panneau **« Variables requises par le compose »** listant chaque
   clé avec :
   - un **champ de saisie** (masqué pour les noms sensibles : `*KEY*`,
     `*SECRET*`, `*TOKEN*`, `*PASSWORD*`),
   - un badge d'état : **`absente`** (rouge) ou **`valeur manquante`** (orange) ;
3. met à jour le `.env` en direct à chaque frappe (la ligne existante est
   remplacée, sinon la clé est ajoutée).

Les variables avec valeur par défaut (`${VAR:-défaut}`) sont ignorées : elles
n'ont pas besoin d'être déclarées.

### 🔧 Backend
- `ValidationResult` expose désormais `missing_env[]` et `empty_env[]`
  (triés, sans doublon) en plus des avertissements.
- Distinction explicite des messages :
  - `variable ${X} référencée mais absente du .env`
  - `variable ${X} déclarée dans le .env sans valeur`

## Version 0.8.0 (2026-11-26)

### 🎉 Validation YAML en direct dans la création de compose (sans IA)

La modale « Nouveau conteneur » valide désormais le compose **pendant la
saisie** (debounce 350 ms), avant tout déploiement :

- Bandeau vert **« Compose valide »** avec la liste des services détectés.
- Bandeau rouge **« Compose invalide »** listant chaque erreur avec **numéro de
  ligne**, champ concerné et message.
- Bandeau orange pour les **avertissements** (non bloquants).
- Le bouton **« Créer le conteneur » est désactivé** tant que le compose est
  invalide, avec une infobulle explicative.
- La création renvoie aussi une **erreur JSON structurée** (HTTP 400) si la
  validation échoue, donc aucun appel au moteur pour un compose cassé.

### 🔍 Contrôles effectués

**Syntaxe / structure**
- YAML invalide → erreur avec ligne.
- **Tabulations** dans l'indentation (interdites en YAML) → erreur ciblée.
- **Indentation impaire** (ex. 3 espaces) → erreur pointant la **vraie ligne
  fautive** (le parseur YAML, lui, signale souvent la ligne précédente).
- Espaces en fin de ligne → avertissement.
- Racine non-objet, section `services` absente ou vide.

**Sémantique (ce qui ferait échouer le déploiement)**
- `image` obligatoire et **référence d'image validée** (`distribution/reference`) ;
  `build` signalé comme non supporté.
- `container_name` valide, `restart` dans l'énumération autorisée.
- **Ports** : format `hôte:conteneur` / `conteneur` / `ip:hôte:conteneur`,
  numéricité, protocole, syntaxe longue (`target`/`published`).
- **Variables d'environnement** : `CLE=VALEUR`, noms valides.
- **Volumes** : espaces interdits, volume anonyme signalé.
- `command`/`entrypoint`/`labels` : type attendu.
- `version` obsolète et clés de premier niveau inconnues → avertissements.
- **`${VAR}` absente du `.env`** → avertissement explicite.
- Plusieurs services → avertissement (seul celui du nom sera créé).

### 🔧 Backend
- `POST /api/docker/compose/validate` → résultat structuré
  (`valid`, `errors[]`, `warnings[]`, `services[]`).
- Nouveau fichier `backend/internal/updater/validate.go`.

## Version 0.7.1 (2026-11-26)

### 🎉 Réseaux et ports visibles dans la liste des conteneurs

Deux nouvelles colonnes dans le tableau **Podman, Docker & Swarm → Conteneurs** :

- **Réseaux** — nom du réseau si unique ; sinon petite icône réseau avec `…`
  et une **infobulle au survol** listant tous les réseaux.
- **Ports** — mapping si unique (ex. `8080:80/tcp`) ; sinon icône avec `…` et
  infobulle listant tous les ports.
- Aucun réseau/port → `—`.

#### Backend
- `GET /api/docker/containers` renvoie désormais `networks` et `ports`.
- Les réseaux proviennent de `NetworkSettings.Networks` et, lorsque l'API
  Docker-compatible de Podman les renvoie vides (Podman rootless), du **mode
  réseau** (`pasta`, `bridge`, `host`, réseau personnalisé).
- Les ports sont normalisés : ports publiés `hôte:conteneur/proto`, ports
  seulement exposés `conteneur/proto`, sans doublon.

## Version 0.7.0 (2026-11-26)

### 🎉 Création de conteneur depuis un compose (sans IA)

Bouton **« Nouveau »** (orange) à côté d'« Actualiser » sur la page Podman,
Docker & Swarm. Il ouvre la même interface que l'éditeur Compose, mais :

- le **nom du conteneur se saisit dans un champ** placé après le titre de la
  modale (au lieu de reprendre le nom du conteneur courant) ;
- les onglets **docker-compose.yml** et **.env** sont disponibles, avec
  **substitution `${VAR}`** depuis le `.env` ;
- le squelette YAML se met à jour automatiquement avec le nom saisi tant que
  l'utilisateur n'a pas modifié le contenu.

À la création :
1. Pull best-effort de l'image.
2. Création puis démarrage du conteneur avec le nom saisi (`restart`, `ports`,
   `volumes`, `environment`, `labels`, `command`, `network_mode`).
3. Contrôle de santé (jusqu'à 40 s) ; en cas d'échec le conteneur est
   **supprimé** et les logs + diagnostic IA sont affichés.
4. Le compose et le `.env` sont **enregistrés** pour ce conteneur (onglet Compose
   pré-rempli ensuite).
5. Nom validé (`[a-zA-Z0-9][a-zA-Z0-9_.-]*`), sinon HTTP 400.

### 🔧 Moteur de conteneurs reconnu

- La colonne **Moteur** affiche désormais le runtime réel. Il est détecté au
  démarrage via `/version` (Podman expose un composant « Podman Engine »),
  avec repli sur `/info` ; journalisé : `Container engine detected: podman`.
- `runtime` est renvoyé par `GET /api/docker/containers` (auparavant absent, la
  colonne affichait « inconnu »).

### 🐛 Correctifs
- `buildConfig` plantait (nil pointer) lors d'une création depuis zéro, car
  `ContainerJSON.ContainerJSONBase` est nil quand aucun conteneur n'existe.

## Version 0.6.1 (2026-11-26)

### 🛡 Sécurisation de la suppression d'images

- `DELETE /api/docker/images/{id}` **refuse (HTTP 409)** la suppression d'une
  image utilisée par un conteneur, sauf `?force=true` explicite.
  *Motif : `podman rmi --force` supprime aussi les conteneurs qui utilisent
  l'image — une suppression « inoffensive » pouvait donc détruire un conteneur
  en production.*
- L'interface désactive également le bouton « Supprimer » pour les images
  utilisées, avec une infobulle explicative.

### 📊 Mesures d'espace corrigées

- La taille des images affichée utilise désormais le **stockage unique de
  couches** (`LayersSize`) au lieu de la somme des tailles d'images, qui
  comptait plusieurs fois les couches partagées (surévaluation ×10).
- L'espace « récupérable » ne compte que les images réellement supprimables
  (celles listées par `ImageList`), en excluant les couches intermédiaires
  figées par leur image enfant.
- L'espace réellement libéré par un nettoyage est **mesuré avant/après** sur ce
  stockage unique, et non estimé par addition.
- Le nettoyage s'exécute en **plusieurs passes** : supprimer une image peut
  révéler ses parents comme nouvelles images inutilisées.
- `deleted` est toujours renvoyé sous forme de tableau (jamais `null`).

## Version 0.6.0 (2026-11-26)

### 🎉 Sous-menu de gestion des ressources

Le module « Podman, Docker & Swarm » possède désormais un **sous-menu** :

`Conteneurs` | `Réseaux` | `Images` | `Volumes`

#### Barre « Espace disque » (visible sur tous les onglets)
- Taille des **images**, des **couches d'écriture des conteneurs**, des
  **volumes** et du **cache de build**.
- Met en avant l'**espace récupérable** (images et volumes non utilisés).
- Bouton **« Récupérer l'espace »** : enchaîne la suppression des images
  inutilisées, des volumes non montés et du cache de build, puis affiche la
  quantité réellement libérée.

#### Réseaux
- Liste (nom, driver, portée, nombre de conteneurs, utilisé/inutilisé).
- Création : nom, driver (`bridge`, `macvlan`, `ipvlan`, `overlay`), sous-réseau,
  option « réseau interne ».
- Suppression unitaire (désactivée si des conteneurs y sont attachés) et
  **« Supprimer les inutilisés »**.

#### Images
- Liste triée **images inutilisées d'abord**, avec taille, date, tags/`dangling`,
  et badge **utilisée / inutilisée** calculé à partir des conteneurs existants.
- Compteur « N inutilisée(s) · X Go » et filtre **« Inutilisées seulement »**.
- Suppression unitaire forcée + **« Supprimer les inutilisées »** avec espace
  récupéré.

#### Volumes (données temporaires des conteneurs)
- Liste (nom, driver, taille, nombre de montages, utilisé/inutilisé).
- Création et suppression (forcée, avec confirmation explicite sur la perte de
  données).
- **« Supprimer les non utilisés »** avec espace récupéré.

### 🔧 Backend
- `GET/POST /api/docker/networks`, `DELETE /api/docker/networks/{id}`,
  `POST /api/docker/networks/prune`
- `GET /api/docker/images`, `DELETE /api/docker/images/{id}`,
  `POST /api/docker/images/prune`
- `GET/POST /api/docker/volumes`, `DELETE /api/docker/volumes/{name}`,
  `POST /api/docker/volumes/prune`
- `GET /api/docker/disk-usage`, `POST /api/docker/build-cache/prune`
- Nouveau fichier `backend/cmd/server/resources.go`

### ⚠️ Notes Podman
- Le cache de build n'est pas exposé par l'API compatible Docker de Podman :
  l'endpoint répond `supported: false` avec un message explicite au lieu d'une
  erreur.
- Les volumes et images « utilisés » sont déterminés par croisement avec les
  conteneurs existants (pas seulement par les compteurs du démon).

## Version 0.5.0 (2026-11-26)

### 🎉 « Enregistrer et Appliquer » dans l'éditeur de compose

La modale Compose dispose maintenant de deux actions :

- **Enregistrer** — persiste `docker-compose.yml` et `.env` (inchangé).
- **Enregistrer et Appliquer** — enregistre **puis recrée le conteneur** à partir
  de la définition éditée, avec **rollback automatique**.

#### Déroulé de l'application
1. Enregistrement de la définition (jamais perdue, même si l'application échoue).
2. Pull best-effort de l'image (ignore les échecs réseau).
3. Le conteneur actuel est **renommé en sauvegarde** (`<nom>__guard_backup_<ts>`)
   puis arrêté.
4. Création/démarrage du nouveau conteneur avec la config du compose.
5. Attente jusqu'à 40 s : le conteneur doit rester **running** et devenir
   **healthy** s'il déclare un healthcheck.
6. **Succès** → la sauvegarde est supprimée.
   **Échec** (création, démarrage, sortie non nulle, unhealthy) → le nouveau
   conteneur est supprimé, la sauvegarde est renommée et redémarrée, et les logs
   du conteneur en échec sont renvoyés avec un diagnostic IA.

#### Champs du compose pris en charge
`image`, `container_name`, `restart`, `ports` (court et long), `volumes`,
`environment` (liste et map), `labels`, `command`, `network_mode`, avec
**substitution `${VAR}` / `${VAR:-default}`** depuis l'onglet `.env`.

#### Sécurité
- **Refus de recréer le conteneur Homelab Guard lui-même** (il sert la requête) :
  HTTP 400 avec message explicite — l'application doit venir d'un agent distant.
- Le diagnostic IA renvoie désormais le **statut HTTP et le corps** en cas
  d'erreur du worker (au lieu de « Failed to parse AI diagnosis »).
- Correctif : les champs cgroup v1 renvoyés par Podman (`MemorySwappiness`,
  `OomKillDisable`) sont retirés lors de la recréation, sinon crun échoue
  (`cannot set memory swappiness with cgroupv2`).

### 🔧 Backend
- `POST /api/docker/containers/{id}/compose/apply`
- Nouveau module `internal/updater/compose.go` (parsing YAML + moteur d'apply)

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
