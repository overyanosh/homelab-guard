package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/overyanosh/homelab-guard/backend/internal/oidc"
)

// registerAuthRoutes wires the optional OIDC login flow. When the provider is
// nil (no OIDC_* environment), only /auth/config is exposed and reports
// enabled=false, so the UI never shows a login page.
func registerAuthRoutes(mux *http.ServeMux, provider *oidc.Provider) {
	mux.HandleFunc("GET /auth/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": provider.Enabled(),
		})
	})

	if !provider.Enabled() {
		mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "enabled": false})
		})
		return
	}

	mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := provider.SessionFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "enabled": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"enabled":       true,
			"email":         sess.Email,
			"name":          sess.Name,
		})
	})

	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		ep, err := provider.Endpoints(r.Context())
		if err != nil {
			log.Printf("oidc: %v", err)
			http.Error(w, "OIDC indisponible : "+err.Error(), http.StatusBadGateway)
			return
		}
		state := oidc.RandomToken(24)
		nonce := oidc.RandomToken(24)
		verifier := oidc.RandomToken(48)
		redirectURL := provider.RedirectURL(r)

		provider.SetState(w, r, oidc.State{
			State:       state,
			Nonce:       nonce,
			Verifier:    verifier,
			RedirectURL: redirectURL,
		})
		http.Redirect(w, r, provider.AuthCodeURL(ep, state, nonce, verifier, redirectURL), http.StatusFound)
	})

	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		st, ok := provider.TakeState(w, r)
		if !ok {
			http.Error(w, "session de connexion expirée, recommencez", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("error") != "" {
			http.Error(w, "connexion refusée : "+r.URL.Query().Get("error"), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != st.State {
			http.Error(w, "state OIDC invalide", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "code d'autorisation absent", http.StatusBadRequest)
			return
		}

		ep, err := provider.Endpoints(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		accessToken, err := provider.Exchange(r.Context(), ep, code, st.Verifier, st.RedirectURL)
		if err != nil {
			log.Printf("oidc: %v", err)
			http.Error(w, "échange de jeton échoué", http.StatusBadGateway)
			return
		}
		claims, err := provider.Userinfo(r.Context(), ep, accessToken)
		if err != nil {
			log.Printf("oidc: %v", err)
			http.Error(w, "récupération du profil échouée", http.StatusBadGateway)
			return
		}
		if !provider.Allowed(claims) {
			log.Printf("oidc: accès refusé pour %q (allowlist)", claims.Email)
			http.Error(w, "ce compte n'est pas autorisé à accéder à Homelab Guard", http.StatusForbidden)
			return
		}

		provider.IssueSession(w, r, claims)
		log.Printf("oidc: connexion réussie pour %q", claims.Email)
		http.Redirect(w, r, "/", http.StatusFound)
	})

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		provider.ClearSession(w, r)
		w.WriteHeader(http.StatusNoContent)
	})
}

// authMiddleware protects API and websocket routes when OIDC is enabled. The SPA
// shell and static assets stay public so the login page can be rendered.
func authMiddleware(next http.Handler, provider *oidc.Provider) http.Handler {
	if !provider.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !needsAuthentication(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := provider.SessionFromRequest(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":   "authentication required",
				"login":   "/auth/login",
				"enabled": true,
			})
			return
		}
		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

func needsAuthentication(path string) bool {
	// Public: auth flow, health/version probes, SPA shell, static assets, agent
	// enrollment (its own bearer token auth).
	switch {
	case strings.HasPrefix(path, "/auth/"):
		return false
	case path == "/api/health", path == "/api/version":
		return false
	case path == "/ws/agent":
		return false
	case strings.HasPrefix(path, "/assets/"):
		return false
	case path == "/", path == "/index.html", path == "/favicon.svg", path == "/logo.svg":
		return false
	}
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/")
}
