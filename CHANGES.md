# Changelog - Homelab Guard

## Version 0.9.13 (2026-11-27)

### 🏷 Labels `guard.*` ajoutés au compose de Homelab Guard

Le compose de Homelab Guard déclare désormais ses propres labels de gestion,
dans `deploy/docker-compose.yml`, `deploy/docker-compose-podman.yml` **et** dans
le compose stocké (modale Compose du conteneur `homelab-guard`) :

```yaml
    labels:
      guard.enable: "true"
      guard.group: "platform"
      guard.update.mode: "notify"
      guard.update.schedule: "0 4 * * *"
      guard.test.strategy: "healthcheck"
      guard.test.timeout: "120s"
      guard.ai.context: "Console Homelab Guard : API/UI sur 9015 + worker IA sur 8001."
      guard.backup.pre-update: "false"
```

- `notify` : les mises à jour sont **signalées** mais jamais appliquées
  automatiquement au conteneur qui sert l'interface.
- `healthcheck` : l'image ne déclarant pas de `HEALTHCHECK`, le moteur retombe
  proprement sur son contrôle « conteneur stable » après 5 s.
- Vérifié : un conteneur créé avec ces labels est bien lu comme
  `enabled=true, group=platform, mode=notify, schedule=0 4 * * *,
  strategy=healthcheck, timeout=120s`.

**À noter** : les labels ne seront actifs sur le conteneur `homelab-guard` qu'au
prochain **redémarrage/recréation** (l'auto-application est refusée par
l'interface pour ne pas couper le service). Le build local a été mis à jour,
`Test` n'a **pas** été publié (à la demande).

## Version 0.9.12 (2026-11-27)

### 🧹 Variables OIDC en commentaire + validateur corrigé

- `deploy/docker-compose.yml`, `deploy/docker-compose-podman.yml` et
  **`deploy/.env`** contiennent désormais un bloc **OIDC entièrement commenté**
  (il suffit de décommenter pour activer la connexion).
- Le **compose stocké du conteneur `homelab-guard`** (visible dans la modale
  Compose) est lui aussi un modèle propre avec le bloc OIDC commenté — et sans
  secret masqué (`MASTER_KEY=***`) qui aurait cassé le chiffrement au prochain
  apply.
- **Correctif du validateur** : les variables citées **dans des commentaires**
  (`# - MASTER_KEY=${MASTER_KEY}`) ne déclenchent plus de faux avertissement
  « variable absente du .env ». Le scan des `${VAR}` ignore désormais les
  commentaires YAML (en respectant les chaînes entre guillemets).

Vérifié : le compose de `homelab-guard` valide **0 erreur / 0 avertissement**.

## Version 0.9.11 (2026-11-27)

### 🔐 Connexion OIDC optionnelle

Une page de connexion **OpenID Connect** est ajoutée, **activée uniquement si
`OIDC_ISSUER` et `OIDC_CLIENT_ID` sont fournis** dans l'environnement du
conteneur (compose). Sans ces variables, le comportement reste identique
(accès libre, aucune page de connexion, `enabled: false`).

#### Configuration
| Variable | Requis | Rôle |
|---|---|---|
| `OIDC_ISSUER` | ✅ | URL de l'émetteur (realm Keycloak, Authentik, Authelia…) |
| `OIDC_CLIENT_ID` | ✅ | Client ID |
| `OIDC_CLIENT_SECRET` | ⬜ | Secret client |
| `OIDC_REDIRECT_URL` | ⬜ | Callback (défaut `<scheme>://<host>/auth/callback`) |
| `OIDC_SCOPES` | ⬜ | Défaut `openid profile email` |
| `OIDC_ALLOWED_EMAILS` / `OIDC_ALLOWED_DOMAINS` | ⬜ | Allowlist |
| `OIDC_SESSION_SECRET` | ⬜ | Signature du cookie (défaut `MASTER_KEY`) |

#### Sécurité
- Flow **authorization code + PKCE (S256)**, `state` et `nonce` aléatoires dans
  un cookie signé **à usage unique**.
- Échange de code côté serveur, puis lecture des claims via `userinfo`.
- Session **HMAC-SHA256** (12 h), cookie `HttpOnly`, `SameSite=Lax`, `Secure`
  derrière TLS.
- Allowlist par email/domaine (refus **403**).
- Routes protégées : `/api/*` (sauf `health`/`version`) et `/ws/*` (sauf
  `/ws/agent`, authentifié par jeton).
- SPA et assets statiques publics, pour afficher la page de connexion.

#### Interface
- Page de connexion dédiée (« Se connecter avec OIDC ») affichée seulement
  quand OIDC est actif et la session absente.
- Email de l'utilisateur + « Se déconnecter » en bas de la barre latérale.
- Vérification de session toutes les 60 s (retour automatique à l'écran de
  connexion à expiration).

#### Backend
- Nouveau package `internal/oidc` (discovery, PKCE, échange, userinfo, session).
- Nouveau fichier `cmd/server/auth.go` (routes + middleware).
- Endpoints : `GET /auth/config`, `GET /auth/login`, `GET /auth/callback`,
  `GET /auth/me`, `POST /auth/logout`.

#### Tests (fournisseur OIDC simulé)
`/auth/config` → `enabled:true` ; API protégée → **401** sans session ;
`/auth/login` → **302** vers l'IdP avec `code_challenge` S256, `state`, `nonce` ;
`/auth/callback` (code + state valides) → **302 /** + cookie de session ;
`/auth/me` → `authenticated:true, email:alice@example.com` ; API → **200** ;
state invalide → **400** ; allowlist → **403** ; logout → API **401**.

## Version 0.9.10 (2026-11-27)

### 📐 Modales compose / stack : hauteur 90 % et barre d'actions toujours visible

Avec un gros compose et beaucoup d'erreurs, la modale dépassait l'écran et le
bouton de validation devenait inatteignable.

Corrections sur les **trois** modales (Nouveau, Compose d'un conteneur, Stack) :

- conteneur en colonne `max-h-[90vh]` avec marge `p-4` sur l'overlay ;
- **en-tête fixe** (`shrink-0`) ;
- **zone centrale scrollable** (`flex-1 min-h-0 overflow-y-auto`) contenant
  l'éditeur, le panneau de validation, les variables `.env` et le résultat ;
- **barre d'actions fixe** en bas (`shrink-0`, bordure haute) : *Créer* /
  *Enregistrer* / *Enregistrer et Appliquer* / *Déployer la stack* restent
  **toujours accessibles**, quel que soit le nombre d'erreurs ;
- éditeurs dimensionnés en `min(38vh, 420px)` pour laisser de la place au
  panneau d'erreurs et au pied de modale.

## Version 0.9.9 (2026-11-27)

### 🔢 Numéros de ligne dans les éditeurs compose / stacks

Nouveau composant `CodeEditor` (marge de numéros synchronisée au défilement),
utilisé dans les **trois** éditeurs :

- création d'un conteneur simple (« Nouveau »),
- édition du compose d'un conteneur existant,
- création/édition d'une **stack**.

Détails :
- numérotation 1..N alignée sur les lignes réelles (`wrap="off"`, donc pas de
  décalage dû au retour à la ligne) ;
- les **lignes en erreur** (renvoyées par la validation) apparaissent en **rouge
  gras** dans la marge → on trouve immédiatement la ligne signalée ;
- onglet `.env` également numéroté.

### 🧹 Avertissement multi-services supprimé

Le message « *x services définis : sélectionnez le service à déployer* » n'a
plus lieu d'être (le sélecteur de service et la frame Stacks l'ont remplacé) :

- supprimé du validateur backend (`ValidateCompose`) ;
- le cas multi-services reste visible là où il est utile :
  - **Nouveau** : encart orange invitant à utiliser **Stacks** ;
  - **Compose d'un conteneur** : sélecteur de service ;
  - **Stacks** : déploiement de tous les services.

## Version 0.9.8 (2026-11-27)

### 📦 Couverture du graphe de dépendances (npm + Python)

#### Frontend : lockfile npm
- Ajout de **`frontend/package-lock.json`** (généré depuis `package.json`) : le
  graphe de dépendances GitHub analyse désormais **npm**, donc Dependabot peut
  alerter sur les vulnérabilités JS.
- Le Dockerfile utilise **`npm ci`** (build reproductible, versions exactes du
  lockfile) au lieu de `npm install`.
- `npm audit --omit=dev` : **0 vulnérabilité**.

#### Worker IA : dépendances pinnées et auditées
- `ai-worker/requirements.txt` **épinglé** (versions exactes, y compris la
  transitive `starlette`) :
  - `fastapi` 0.115.5 → **0.165.0**
  - `starlette` 0.41.3 → **1.10.0** (corrige **14** avis PYSEC)
  - `uvicorn` 0.32.1 → **0.54.0**
  - `openai` 1.55.3 → **3.60.0**
  - `pydantic` 2.10.2 → **2.14.4**
  - `PyYAML` 6.0.2 → **6.0.4**
- Le Dockerfile met à jour **pip** (≥ 26.2) lors du build : 4 avis PYSEC sur pip
  25.0.1 sont ainsi éliminés.
- `pip-audit -r ai-worker/requirements.txt` : **aucune vulnérabilité connue**.

#### Vérifications après mise à jour
- Worker IA démarre (uvicorn), `GET /health` → `{"status":"ok",...}`.
- `GET /api/ai/config` → `worker_online: true`.
- Validation Pydantic opérationnelle (`422 Field required` sur requête vide).
- Build image OK (`npm ci` + `pip` pinné), conteneur `Up`, `/api/health` OK.

## Version 0.9.7 (2026-11-27)

### 🔒 Sécurité : dépendances mises à jour (Dependabot / govulncheck)

**22 alertes Dependabot** étaient ouvertes sur `backend/go.mod`.

| Paquet | Avant | Après | Alertes résolues |
|---|---|---|---|
| `golang.org/x/crypto` | v0.29.0 | **v0.59.0** | 17 (dont 9 critiques, 2 hautes) |
| `github.com/gorilla/websocket` | v1.5.3 | **v1.5.4-0.20240701034025-d67f41855da4** | 1 (GO-2026-6278, PRNG faible) |
| `github.com/docker/docker` | v27.3.1+incompatible | **v28.5.2+incompatible** | 1 (GHSA-4vq8-7jfc-9cvp) |

- `govulncheck ./...` : **0 vulnérabilité atteignable** dans notre code
  (avant : GO-2026-6278 via `websocket`).
- Client Docker v28 validé contre le socket Podman : conteneurs, inspect,
  compose, logs, images, volumes, réseaux, stacks, start/stop/pause/unpause.

### ⚠️ Alertes restantes (sans correctif publié)

4 alertes `github.com/docker/docker` n'ont **aucune version corrigée**
disponible (`patched: null`) ; la 5ᵉ exigerait `>= 29.3.1`, non publié comme
module Go (le module s'arrête à `v28.5.2+incompatible`).

Elles concernent des points du **démon** (`docker cp`, `PUT /containers/{id}/archive`,
validation de privilèges des plugins), pas le client que nous utilisons.
Exposition actuelle limitée : Homelab Guard parle à un **socket Podman local**
et n'expose aucun de ces endpoints.

### 📋 Couverture du graphe de dépendances

- Frontend : **pas de `package-lock.json`** → npm n'est pas analysé par
  Dependabot. À ajouter pour activer le suivi.
- `ai-worker/requirements.txt` : versions **non figées** → analyse pip peu
  pertinente. À épingler (`pip-audit`).

## Version 0.9.6 (2026-11-27)

### 🐛 Pages blanches (Réglages Proxmox, Réglages SSH)

Cause : `GET /api/proxmox/clusters` et `GET /api/ssh/hosts` renvoyaient **`null`**
au lieu de `[]` lorsque la liste était vide (slice Go nil sérialisée en JSON
`null`). Le composant faisait ensuite `clusters.map(...)` sur `null` → exception
de rendu → **écran blanc** (React démonte tout l'arbre).

Correctifs :
- **Backend** : les listes vides sont désormais sérialisées en `[]`
  (`ListProxmoxClusters`, `ListSSHHosts`, `ListAgentNodes`).
- **Frontend** : garde-fous `?? []` sur toutes les listes chargées
  (réglages Docker/Proxmox/SSH, conteneurs PVE, images, volumes, réseaux,
  stacks).
- **`ErrorBoundary`** ajouté autour de l'application : une exception dans un
  composant affiche désormais un écran d'erreur lisible (message, « Réessayer »,
  « Recharger la page ») au lieu d'une page blanche.

Vérifié : `/api/proxmox/clusters` et `/api/ssh/hosts` renvoient `[]` ; les pages
de réglages s'affichent.

## Version 0.9.5 (2026-11-27)

### 🧹 Fin des problèmes de cache du frontend

Le shell de l'application (`index.html`) pouvait rester en cache navigateur et
continuer à charger **l'ancien bundle JS**, ce qui donnait l'impression que les
correctifs serveur « ne marchaient pas ».

- `index.html` → `Cache-Control: no-cache, no-store, must-revalidate`
  (+ `Pragma`, `Expires`).
- Assets hachés `/assets/*` → `public, max-age=31536000, immutable`
  (leur nom change à chaque build, donc aucun risque).

### 🆕 Détection de version obsolète

- `GET /api/version` → `{ version, asset, engine, started_at }`.
  `asset` est le nom du bundle référencé par l'`index.html` embarqué.
- L'interface compare ce nom au script qu'elle exécute (vérifié au chargement et
  toutes les 60 s). En cas d'écart, un **bandeau orange** propose
  « Recharger maintenant » (rechargement avec paramètre anti-cache).
- La **version et le nom du bundle** sont affichés en bas de la barre latérale :
  on voit immédiatement si le navigateur exécute l'ancienne version.

Exemple :

```
Homelab Guard v0.9.5
index-BsAhWFPv.js
```

## Version 0.9.4 (2026-11-27)

### 🗑 Suppression de stack : tous les conteneurs associés sont bien supprimés

La suppression d'une stack s'appuie désormais sur une détection **élargie** et
un teardown **résilient** :

- reconnaissance des conteneurs par **`guard.stack`** *ou*
  **`com.docker.compose.project`** (insensible à la casse) ;
- prise en compte des conteneurs **arrêtés, en pause, renommés**
  (`container_name`) ou en erreur ;
- un échec sur un conteneur **n'interrompt plus** la suppression des autres :
  chaque échec est collecté et renvoyé ;
- la définition stockée n'est supprimée que si **aucun** échec n'est survenu ;
- réponse détaillée : `{ "stack": "...", "removed": [...], "failed": [...] }` ;
- l'interface affiche un **bilan** (« 2 conteneur(s) supprimé(s) : a, b ») ou la
  liste des échecs ;
- journalisation serveur : `stack down: name="x" matched=2 removed=2 failed=0`
  pour diagnostiquer immédiatement un cas où aucun conteneur ne correspond.

Testé avec succès sur : conteneur au `container_name` explicite, conteneur
arrêté (`Exited 137`), conteneur en pause → tous supprimés.

## Version 0.9.3 (2026-11-27)

### ▶️⏸⏹ Actions de cycle de vie des conteneurs

Trois boutons d'action dans la colonne **Actions** du tableau, à côté de
« Test & Update » et de la corbeille :

| Bouton | Icône | Actif quand | Action |
|---|---|---|---|
| **Démarrer / Reprendre** | ▶ | conteneur arrêté (`exited`, `created`) ou **suspendu** | `start` (ou `unpause` si suspendu) |
| **Suspendre** | ⏸ | conteneur en cours d'exécution | `pause` ; devient ▶ « Reprendre » si déjà suspendu |
| **Arrêter** | ⏹ | en cours ou suspendu | `stop` (timeout 10 s) |

Les boutons sont **désactivés** quand l'action n'a pas de sens (démarrer un
conteneur déjà lancé, arrêter un conteneur arrêté, etc.).

### 🛡 Protection
- Les actions `stop`, `pause`, `unpause` et `start` sur le **conteneur Homelab
  Guard lui-même** sont refusées (**409**) : arrêter ou suspendre le conteneur
  couperait l'interface qui sert la requête.

### 🔧 Backend
- `POST /api/docker/containers/{id}/start`
- `POST /api/docker/containers/{id}/stop`
- `POST /api/docker/containers/{id}/pause`
- `POST /api/docker/containers/{id}/unpause`

## Version 0.9.2 (2026-11-27)

### 🔧 CI/CD : deux workflows, deux branches (plus de runs sur `main`)

L'unique workflow `release.yml` se déclenchait sur chaque push `main`, sur les
tags et sur les pull requests — trop de runs pour rien.

| Branche  | Rôle                    | Versionnement  | Workflow             |
|----------|-------------------------|----------------|----------------------|
| `main`   | compilation **locale**  | `vX.X.1`       | ❌ **aucun**         |
| `Test`   | tests / pré-release     | `vX.1.<build>` | `publish-test.yml`   |
| `latest` | version **stable**      | `v1.<build>`   | `publish-latest.yml` |

- `publish-test.yml` → `:test`, `:0.1`, `:0.1.<run>` (+ agent).
- `publish-latest.yml` → `:latest`, `:1`, `:1.<run>` (+ agent).
- Plus de déclenchement sur tags ni sur pull requests ; `concurrency` annule le
  run précédent de la même branche.
- Caches de build séparés par branche.
- `release.yml` supprimé.
- Documentation : `docs/BRANCHES.md`.

## Version 0.9.1 (2026-11-27)

### 🏷 Labels (et commande) inclus dans le compose généré

Le compose généré depuis un conteneur omettait complètement les `labels` — vous
ne rêviez pas. Il inclut désormais :

- **`labels`** : labels utilisateur et `guard.*` (`guard.enable`, `guard.group`,
  `guard.stack`, `guard.service`, …), triés ;
- **`command`** et **`entrypoint`** lorsqu'ils sont définis sur le conteneur.

Sont filtrés les labels de « bruit » issus des images de base et des outils
(`io.buildah.version`, `io.container.manager`, `org.opencontainers.image.*`,
`org.systemd.property.*`, `io.buildkit.*`) pour garder le fichier lisible.

Conséquence : un cycle **générer → appliquer** conserve les labels et la
commande, donc les conteneurs restent **gérés par `guard.*`** après recréation.

Vérifié :
- génération → `labels: app.role=web`, `guard.enable=true`, `guard.group=prod`
  et `command: ["sh","-c","sleep 300"]` ;
- apply du compose généré → labels et commande préservés ;
- création depuis un compose avec `labels:` → labels appliqués au conteneur.

## Version 0.9.0 (2026-11-27)

### 🐛 « Actualiser » inopérant, conteneurs invisibles, « fantômes »

Cause : les réponses JSON n'avaient **aucun en-tête de cache**. Le navigateur
resservait une liste périmée, d'où :
- bouton **Actualiser** sans effet visible ;
- conteneurs créés mais **pas affichés** (« il n'en est rien ») ;
- noms **déjà pris** par des conteneurs qui n'existaient plus (fantômes).

Correctifs :
- `Cache-Control: no-store, max-age=0` sur toutes les réponses JSON ;
- `cache: 'no-store'` sur les requêtes de listes côté frontend.

### ↩️ Retour en arrière : « Nouveau » = **un seul conteneur**

Le bouton **« Nouveau »** crée de nouveau un **conteneur simple** :
- pas de sélecteur de service, le compose doit déclarer **un seul service** ;
- si plusieurs services sont présents, la création est refusée avec un message
  qui renvoie vers **Stacks** ;
- les variables `.env` manquantes sont toujours ajoutées automatiquement et la
  validation en direct reste active.

### 🆕 Bouton **« Stacks »** (compose multi-services)

Nouveau bouton à côté de « Nouveau », plus un onglet **Stacks** dans le
sous-menu Podman/Docker/Swarm.

- **Déployer une stack** : chaque service devient un conteneur nommé
  `container_name` ou `<stack>-<service>`, étiqueté
  `guard.stack=<stack>`, `guard.service=<service>` et
  `com.docker.compose.project/service`.
- **Redéploiement idempotent** : les conteneurs de la stack sont recréés ; un
  conteneur portant le même nom mais **hors stack** est signalé sans être touché.
- **Liste des stacks** : services, état actif/inactif, compteur, boutons
  « Modifier / Redéployer » et « Arrêter et supprimer ».
- Compose et `.env` **persistés** en base (`stacks`), donc rechargeables.
- Validation et saisie des variables `.env` manquantes identiques aux autres
  modales.

### 🗑 Suppression d'un conteneur depuis l'interface

- Bouton corbeille dans la colonne Actions (avec confirmation) pour nettoyer un
  conteneur résiduel et libérer son nom.
- `DELETE /api/docker/containers/{id}` refuse (**409**) la suppression du
  conteneur Homelab Guard lui-même.

### 🔧 Backend
- `GET/POST /api/docker/stacks`, `GET /api/docker/stacks/{name}`,
  `POST /api/docker/stacks/{name}/down`, `DELETE /api/docker/stacks/{name}`
- `DELETE /api/docker/containers/{id}`
- `updater.DeployStack`, `updater.RemoveStack`, `updater.ParseComposeServices`
- Table `stacks`

## Version 0.8.4 (2026-11-27)

### 🎉 Conflits de nom de conteneur détectés avant création

Créer un second conteneur (`tokar-2`) à partir d'un service existant pouvait
échouer avec le message brut de Podman :

```
container create: creating container storage: the container name "tokar" is
already in use by ...
```

Corrections :

- **Vérification préalable** côté backend : si le nom demandé est déjà utilisé,
  renvoi immédiat d'un message actionnable avec l'ID fautif :
  `un conteneur nommé "tokar" existe déjà (id 68673643bb7a) : choisissez un autre
  nom ou supprimez-le d'abord`.
- **Interface** : dès que le nom saisi correspond à un conteneur existant, un
  bandeau rouge l'indique et propose un nom libre en un clic
  (**« Utiliser « tokar-3 » »**). Le bouton « Créer » est désactivé.
- `autoComplete="off"` sur le champ nom pour éviter qu'un remplissage automatique
  du navigateur ne réinjecte un ancien nom (source probable du conflit).
- **Journalisation** de chaque création :
  `compose create: name="tokar-2" service="tokar" image="..."` pour diagnostiquer
  les cas limites.

### ✅ Précision de comportement

Le **nom saisi dans le formulaire est toujours prioritaire** sur un éventuel
`container_name` déclaré dans le service composé (vérifié : service avec
`container_name: tokar` créé sous le nom `tokar-3`).

## Version 0.8.3 (2026-11-27)

### 🎉 Compose multi-services : choix du service à déployer

Ajouter un service à un compose existant ne provoque plus d'erreur
`service "X" not found in compose file`.

- La résolution du service est désormais plus tolérante, dans cet ordre :
  1. clé de service égale au nom du conteneur ;
  2. service dont `container_name` correspond au conteneur ;
  3. unique service du fichier ;
  4. sinon **message explicite** listant les services disponibles.
- Un **sélecteur « Service »** apparaît dans les deux modales dès que le compose
  contient plusieurs services :
  - présélection automatique sur le service correspondant au conteneur ;
  - si aucun ne correspond, l'action (**Créer** / **Enregistrer et Appliquer**)
    reste **désactivée** jusqu'à ce qu'un service soit choisi — pas d'application
    accidentelle d'une autre définition.
- Le service sélectionné est transmis au backend (`service` dans le corps de
  `POST /api/docker/containers/compose` et
  `POST /api/docker/containers/{id}/compose/apply`).
- L'avertissement est reformulé : « N services définis : sélectionnez le service
  à déployer dans la liste ».

### 💡 Créer le nouveau service sous un autre nom

La modale « Nouveau conteneur » permet de **créer un conteneur portant un nom
différent du service** (ex. service `redis` → conteneur `redis-copy`), ce qui
permet de déployer un second service du même fichier compose.

## Version 0.8.2 (2026-11-27)

### 🎉 Validation appliquée aussi à « Enregistrer et Appliquer »

La modale Compose des conteneurs **existants** profite désormais de la même
validation que la création :

- Analyse **en direct** (debounce 350 ms) du compose et du `.env`.
- Bandeau **vert** (valide + services), **rouge** (erreurs avec ligne/champ) et
  **orange** (avertissements).
- **Clés `.env` manquantes ajoutées automatiquement**, avec compteur sur
  l'onglet `.env` et champs de saisie inline (masqués si nom sensible).
- Le bouton **« Enregistrer et Appliquer » est désactivé** tant que le compose
  est invalide ou en cours d'analyse, avec infobulle explicative.
- **« Enregistrer »** reste disponible : il est possible de conserver un
  brouillon non valide sans toucher au conteneur.

### 🛡 Backend
- `POST /api/docker/containers/{id}/compose/apply` **valide avant toute action** :
  si le compose est invalide, réponse **HTTP 400** avec le résultat structuré et
  le conteneur en cours n'est **pas modifié** (ni renommé, ni arrêté).

### ♻️ Refactor frontend
- Nouveau composant partagé `components/ComposeValidation.tsx`
  (`ComposeValidationPanel`, `EnvGapBadge`, helpers `envKeys` / `setEnvValue` /
  `addEnvKeys`), utilisé par les deux modales — plus de duplication.

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
