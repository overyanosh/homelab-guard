package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/overyanosh/homelab-guard/backend/internal/agenthub"
	"github.com/overyanosh/homelab-guard/backend/internal/store"
	"github.com/overyanosh/homelab-guard/backend/internal/updater"
)

// registerNodeComposeRoutes exposes compose editing, container creation and
// stacks for REMOTE agents. Definitions are stored server-side (keyed by node)
// while the Docker operations are executed by the agent.
func registerNodeComposeRoutes(mux *http.ServeMux, hub *agenthub.Hub, dbStore *store.Store) {
	key := func(nodeID int64, suffix string) string {
		return fmt.Sprintf("node%d/%s", nodeID, suffix)
	}
	nodeIDOf := func(r *http.Request) (int64, bool) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		return id, err == nil
	}

	// GET compose of a remote container (stored definition wins, else generated).
	mux.HandleFunc("GET /api/nodes/{id}/containers/{cid}/compose", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		raw, err := hub.SendCommand(nodeID, "compose_get", map[string]any{
			"container_id": r.PathValue("cid"),
		}, 20*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		var payload struct {
			Service   string `json:"service"`
			YAML      string `json:"yaml"`
			Source    string `json:"source"`
			Env       string `json:"env"`
			EnvExists bool   `json:"env_exists"`
		}
		_ = json.Unmarshal(raw, &payload)

		if stored, err := dbStore.GetCompose(key(nodeID, payload.Service)); err == nil && strings.TrimSpace(stored) != "" {
			payload.YAML = stored
			payload.Source = "stored"
		}
		if env, err := dbStore.GetComposeEnv(key(nodeID, payload.Service)); err == nil && strings.TrimSpace(env) != "" {
			payload.Env = env
			payload.EnvExists = true
		}
		writeJSON(w, http.StatusOK, payload)
	})

	// PUT stores the definition server-side.
	mux.HandleFunc("PUT /api/nodes/{id}/containers/{cid}/compose", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		var req struct {
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		service := strings.TrimSpace(req.Service)
		if service == "" {
			// Ask the agent which service this container maps to.
			if raw, err := hub.SendCommand(nodeID, "compose_get", map[string]any{
				"container_id": r.PathValue("cid"),
			}, 20*time.Second); err == nil {
				var p struct {
					Service string `json:"service"`
				}
				_ = json.Unmarshal(raw, &p)
				service = p.Service
			}
		}
		if service == "" {
			service = r.PathValue("cid")
		}
		if err := dbStore.SaveCompose(key(nodeID, service), req.YAML); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := dbStore.SaveComposeEnv(key(nodeID, service), req.Env); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service":    service,
			"yaml":       req.YAML,
			"source":     "stored",
			"env":        req.Env,
			"env_exists": strings.TrimSpace(req.Env) != "",
		})
	})

	// POST applies a compose to a remote container (with rollback).
	mux.HandleFunc("POST /api/nodes/{id}/containers/{cid}/compose/apply", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		var req struct {
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
		}
		raw, err := hub.SendCommand(nodeID, "compose_apply", map[string]any{
			"container_id": r.PathValue("cid"),
			"service":      req.Service,
			"yaml":         req.YAML,
			"env":          req.Env,
		}, 5*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		if req.Service != "" {
			_ = dbStore.SaveCompose(key(nodeID, req.Service), req.YAML)
			_ = dbStore.SaveComposeEnv(key(nodeID, req.Service), req.Env)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	// POST creates a new container on a remote agent (single service).
	mux.HandleFunc("POST /api/nodes/{id}/containers/compose", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		var req struct {
			Name    string `json:"name"`
			Service string `json:"service"`
			YAML    string `json:"yaml"`
			Env     string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "container name is required", http.StatusBadRequest)
			return
		}
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
		} else if len(result.Services) > 1 {
			http.Error(w, "la création simple n'accepte qu'un seul service — utilisez « Stacks »", http.StatusBadRequest)
			return
		}
		raw, err := hub.SendCommand(nodeID, "container_create", map[string]any{
			"name":    req.Name,
			"service": req.Service,
			"yaml":    req.YAML,
			"env":     req.Env,
		}, 5*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		var res struct {
			ContainerName string `json:"container_name"`
		}
		_ = json.Unmarshal(raw, &res)
		if res.ContainerName != "" {
			_ = dbStore.SaveCompose(key(nodeID, res.ContainerName), req.YAML)
			_ = dbStore.SaveComposeEnv(key(nodeID, res.ContainerName), req.Env)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	// Stacks ---------------------------------------------------------------

	mux.HandleFunc("GET /api/nodes/{id}/stacks", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		raw, err := hub.SendCommand(nodeID, "stacks_list", nil, 20*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		_, _ = w.Write(raw)
	})

	mux.HandleFunc("POST /api/nodes/{id}/stacks", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		var req struct {
			Name string `json:"name"`
			YAML string `json:"yaml"`
			Env  string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			http.Error(w, "stack name is required", http.StatusBadRequest)
			return
		}
		if result := updater.ValidateCompose(req.YAML, req.Env); !result.Valid {
			writeJSON(w, http.StatusBadRequest, result)
			return
		}
		raw, err := hub.SendCommand(nodeID, "stack_deploy", map[string]any{
			"name": req.Name,
			"yaml": req.YAML,
			"env":  req.Env,
		}, 10*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		_ = dbStore.SaveCompose(key(nodeID, "stack/"+req.Name), req.YAML)
		_ = dbStore.SaveComposeEnv(key(nodeID, "stack/"+req.Name), req.Env)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	mux.HandleFunc("GET /api/nodes/{id}/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		name := r.PathValue("name")
		yamlDoc, err := dbStore.GetCompose(key(nodeID, "stack/"+name))
		if err != nil {
			http.Error(w, "stack not found", http.StatusNotFound)
			return
		}
		env, _ := dbStore.GetComposeEnv(key(nodeID, "stack/"+name))
		writeJSON(w, http.StatusOK, map[string]string{"name": name, "yaml": yamlDoc, "env": env})
	})

	mux.HandleFunc("DELETE /api/nodes/{id}/stacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := nodeIDOf(r)
		if !ok {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		name := r.PathValue("name")
		raw, err := hub.SendCommand(nodeID, "stack_remove", map[string]any{"name": name}, 3*time.Minute)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if msg := commandError(raw); msg != "" {
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		_ = dbStore.DeleteCompose(key(nodeID, "stack/"+name))
		_ = dbStore.DeleteComposeEnv(key(nodeID, "stack/"+name))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
}
