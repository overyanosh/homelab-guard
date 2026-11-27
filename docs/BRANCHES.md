# Branches, versionnement et publication

Trois branches, **un seul workflow chacune** (ou aucun) — fini les runs CI
déclenchés à chaque push sur `main`.

| Branche  | Rôle                                   | Versionnement   | Workflow                | Images publiées |
|----------|----------------------------------------|-----------------|-------------------------|-----------------|
| `main`   | développement / compilation **locale** | `vX.X.1`        | ❌ aucun                | —               |
| `Test`   | tests / pré-release                    | `vX.1.<build>`  | `publish-test.yml`      | `:test`, `:0.1`, `:0.1.<build>` |
| `latest` | dernière version **stable** diffusée   | `v1.<build>`    | `publish-latest.yml`    | `:latest`, `:1`, `:1.<build>` |

## `main` — compilation locale, aucun workflow

Aucun workflow ne cible `main` : un push sur `main` ne consomme **aucune minute
CI**. Le versionnement local suit `vX.X.1` (le patch reste à `1`), par exemple :

```bash
# compilation et image locales
podman build -t localhost/homelab-guard:0.9.1 .
podman run -d --name homelab-guard --restart=unless-stopped \
  --userns=keep-id:uid=10001,gid=10001 --security-opt label=disable \
  -p 9015:9015 \
  -v "$PWD/data:/data" \
  -v /run/user/1000/podman/podman.sock:/var/run/docker.sock \
  -e DOCKER_HOST=unix:///var/run/docker.sock \
  -e MASTER_KEY="$MASTER_KEY" \
  localhost/homelab-guard:0.9.1
```

## `Test` — pré-release (`vX.1`)

Chaque push sur `Test` déclenche `publish-test.yml` :

1. `go vet ./...` + `go test ./...`
2. build multi-arch (amd64/arm64) des images **serveur** et **agent**
3. publication sur GHCR avec les tags `test`, `0.1` et `0.1.<numéro de run>`

Pull :

```bash
podman pull ghcr.io/overyanosh/homelab-guard:test
```

## `latest` — version stable (`v1`)

Chaque push sur `latest` déclenche `publish-latest.yml` :

1. `go vet ./...` + `go test ./...`
2. build multi-arch (amd64/arm64) des images **serveur** et **agent**
3. publication sur GHCR avec les tags `latest`, `1` et `1.<numéro de run>`

Pull :

```bash
podman pull ghcr.io/overyanosh/homelab-guard:latest
```

## Flux de travail

```
main (dev, local)
  └── merge → Test   → publication :test / 0.1.<build>
        └── merge → latest → publication :latest / 1.<build>
```

Les deux workflows peuvent aussi être lancés manuellement *Actions → Run
workflow* (`workflow_dispatch`), toujours depuis leur branche respective.

## Notes

- Aucun déclenchement sur les tags ni sur les pull requests : deux workflows,
  deux branches, zéro bruit.
- Le **CodeQL « default setup »** de GitHub (workflow dynamique, non présent dans
  le dépôt) a été **désactivé** : il relançait une analyse à chaque push sur
  `main`.
- `concurrency` annule un run en cours lorsqu'un nouveau push arrive sur la même
  branche.
- Les caches de build sont séparés par branche (`scope=server-latest`,
  `scope=server-test`, …) pour éviter les collisions de couches.

## Authentification OIDC (optionnelle)

Activée **uniquement** si `OIDC_ISSUER` et `OIDC_CLIENT_ID` sont fournis dans
l'environnement du conteneur (compose). Sinon, aucune page de connexion n'est
affichée et l'accès reste libre.

| Variable | Requis | Description |
|---|---|---|
| `OIDC_ISSUER` | ✅ | URL de l'émetteur (ex. `https://auth.example.com/realms/homelab`) |
| `OIDC_CLIENT_ID` | ✅ | Client ID enregistré chez l'IdP |
| `OIDC_CLIENT_SECRET` | ⬜ | Secret client (recommandé, flow confidentiel) |
| `OIDC_REDIRECT_URL` | ⬜ | Callback (défaut : `<scheme>://<host>/auth/callback`) |
| `OIDC_SCOPES` | ⬜ | Défaut `openid profile email` |
| `OIDC_ALLOWED_EMAILS` | ⬜ | Liste d'emails autorisés (séparés par virgules) |
| `OIDC_ALLOWED_DOMAINS` | ⬜ | Liste de domaines autorisés |
| `OIDC_SESSION_SECRET` | ⬜ | Secret de signature du cookie (défaut : `MASTER_KEY`) |

Sécurité : flow **authorization code + PKCE (S256)**, `state` et `nonce`
aléatoires stockés dans un cookie signé à usage unique, session **HMAC-SHA256**
expirant après 12 h, cookies `HttpOnly` / `SameSite=Lax` (`Secure` derrière TLS).

Endpoints : `GET /auth/config`, `GET /auth/login`, `GET /auth/callback`,
`GET /auth/me`, `POST /auth/logout`.

Restent publics : `/auth/*`, `/api/health`, `/api/version`, les assets statiques
et `/ws/agent` (authentification par jeton d'agent).
